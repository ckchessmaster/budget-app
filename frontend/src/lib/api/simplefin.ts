import { getAppConfig } from '$lib/config';
import { auth } from '$lib/stores/auth.svelte';

export interface SimpleFINDiscoveredAccount {
	id: string;
	name: string;
	currency: string;
	balance: string;
}

export interface LinkAccountMapping {
	external_account_id: string;
	name: string;
	type: 'checking' | 'savings' | 'cash' | 'credit_card' | 'investment' | 'loan';
	card_mode?: 'PAY_IN_FULL' | 'DEBT_PAYDOWN';
	balance?: string;
}

export interface TriggerBankSyncResponse {
	workflow_id: string;
	status: string;
}

export interface BankSyncResult {
	fetched_count: number;
	inserted_count: number;
	settled_count: number;
	manual_matched_count: number;
	transfers_linked: number;
	normalized_count: number;
	warnings?: string[];
}

/** Status of any workflow the API can start. The endpoint is shared, so `result` is
 *  whatever the workflow returned and each caller narrows it with its own type. */
export interface WorkflowStatus<T> {
	workflow_id: string;
	status: 'RUNNING' | 'RETRYING' | 'COMPLETED' | 'FAILED';
	attempt?: number;
	last_error?: string;
	result?: T;
}

export type BankSyncStatusResponse = WorkflowStatus<BankSyncResult>;

/** What a claim returns before the bank has actually been called. */
export interface ClaimAcceptedResponse {
	workflow_id: string;
	status: string;
}

export interface SimpleFINClaimResult {
	connection_id: string;
	accounts: SimpleFINDiscoveredAccount[];
	accounts_discovered: number;
	warnings?: string[];
}

const CLAIM_POLL_INTERVAL_MS = 1000;
const CLAIM_POLL_TIMEOUT_MS = 180_000;
const DISCOVER_RETRY_DELAYS_MS = [400, 1000];
const MAX_CONSECUTIVE_POLL_ERRORS = 4;
const TRANSIENT_GATEWAY_STATUSES = new Set([502, 503, 504, 520, 522, 524]);

function isTransientGatewayStatus(status: number): boolean {
	return TRANSIENT_GATEWAY_STATUSES.has(status);
}

function isTransientFetchError(err: unknown): boolean {
	return err instanceof TypeError;
}

async function authHeaders(includeJsonContentType = true): Promise<HeadersInit> {
	const headers: Record<string, string> = {};
	if (includeJsonContentType) {
		headers['Content-Type'] = 'application/json';
	}
	const token = await auth.getValidToken();
	if (token) {
		headers['Authorization'] = `Bearer ${token}`;
	}
	return headers;
}

/**
 * Exchange a SimpleFIN setup token for the accounts behind it.
 *
 * The claim is no longer one long HTTP request. The API stages the token, starts a
 * Temporal workflow and answers 202, so a slow or rate-limited bank cannot hold the
 * browser's request open and a 429 is retried by the worker using the bank's own
 * Retry-After instead of being thrown at the user. This function drives that handoff:
 * it polls until the workflow settles and resolves with the discovered accounts.
 */
async function pollSimpleFINAccountsWorkflow(
	workflowId: string,
	failureMessage: string,
	onProgress?: (status: WorkflowStatus<SimpleFINClaimResult>) => void
): Promise<SimpleFINDiscoveredAccount[]> {
	const deadline = Date.now() + CLAIM_POLL_TIMEOUT_MS;
	let consecutiveTransientErrors = 0;

	while (Date.now() < deadline) {
		let status: WorkflowStatus<SimpleFINClaimResult>;
		try {
			status = await fetchWorkflowStatus<SimpleFINClaimResult>(workflowId);
			consecutiveTransientErrors = 0;
		} catch (err) {
			const transient =
				(err instanceof WorkflowStatusError && isTransientGatewayStatus(err.status)) ||
				isTransientFetchError(err);
			if (!transient) {
				throw err;
			}
			consecutiveTransientErrors += 1;
			if (consecutiveTransientErrors > MAX_CONSECUTIVE_POLL_ERRORS || Date.now() >= deadline) {
				throw err;
			}
			await sleep(CLAIM_POLL_INTERVAL_MS);
			continue;
		}

		onProgress?.(status);

		if (status.status === 'COMPLETED') {
			return Array.isArray(status.result?.accounts) ? status.result.accounts : [];
		}
		if (status.status === 'FAILED') {
			throw new Error(status.last_error || failureMessage);
		}
		await sleep(CLAIM_POLL_INTERVAL_MS);
	}

	throw new Error('Timed out waiting for SimpleFIN workflow to finish');
}

export async function claimSimpleFINToken(
	setupToken: string,
	onProgress?: (status: WorkflowStatus<SimpleFINClaimResult>) => void
): Promise<SimpleFINDiscoveredAccount[]> {
	const started = await startSimpleFINClaim(setupToken);
	return pollSimpleFINAccountsWorkflow(started.workflow_id, 'SimpleFIN claim failed', onProgress);
}

/**
 * Fetch accounts from an already-connected SimpleFIN Bridge without claiming a new
 * setup token.
 */
export async function discoverSimpleFINAccounts(
	onProgress?: (status: WorkflowStatus<SimpleFINClaimResult>) => void
): Promise<SimpleFINDiscoveredAccount[]> {
	const { apiBaseUrl } = getAppConfig();
	let started: ClaimAcceptedResponse | null = null;

	for (let attempt = 0; attempt <= DISCOVER_RETRY_DELAYS_MS.length; attempt++) {
		try {
			const res = await fetch(`${apiBaseUrl}/api/v1/bank/simplefin/discover`, {
				method: 'POST',
				headers: await authHeaders(false)
			});
			if (res.ok) {
				started = await res.json();
				break;
			}

			const retryDelay = DISCOVER_RETRY_DELAYS_MS[attempt];
			if (isTransientGatewayStatus(res.status) && retryDelay !== undefined) {
				await sleep(retryDelay);
				continue;
			}

			const err = await res.json().catch(() => ({}));
			throw new Error(err.error || `Failed to discover SimpleFIN accounts (${res.status})`);
		} catch (err) {
			const retryDelay = DISCOVER_RETRY_DELAYS_MS[attempt];
			if (isTransientFetchError(err) && retryDelay !== undefined) {
				await sleep(retryDelay);
				continue;
			}
			throw err;
		}
	}

	if (!started) {
		throw new Error('Failed to discover SimpleFIN accounts');
	}

	return pollSimpleFINAccountsWorkflow(started.workflow_id, 'SimpleFIN account discovery failed', onProgress);
}

/** Start the durable claim. Returns as soon as the API has accepted the work. */
export async function startSimpleFINClaim(setupToken: string): Promise<ClaimAcceptedResponse> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/bank/simplefin/claim`, {
		method: 'POST',
		headers: await authHeaders(),
		body: JSON.stringify({ setup_token: setupToken })
	});
	if (!res.ok) {
		const err = await res.json().catch(() => ({}));
		throw new Error(err.error || `Failed to claim SimpleFIN token (${res.status})`);
	}
	return res.json();
}

/**
 * A status poll that the API refused, carrying the HTTP status so a caller can tell
 * "not answerable yet" apart from "gone wrong".
 */
export class WorkflowStatusError extends Error {
	readonly status: number;

	constructor(message: string, status: number) {
		super(message);
		this.name = 'WorkflowStatusError';
		this.status = status;
	}
}

export async function fetchWorkflowStatus<T>(workflowId: string): Promise<WorkflowStatus<T>> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/bank/workflow-status/${encodeURIComponent(workflowId)}`, {
		headers: await authHeaders(false)
	});
	if (!res.ok) {
		const err = await res.json().catch(() => ({}));
		throw new WorkflowStatusError(
			err.error || `Failed to fetch workflow status (${res.status})`,
			res.status
		);
	}
	return res.json();
}

function sleep(ms: number): Promise<void> {
	return new Promise((resolve) => setTimeout(resolve, ms));
}

export async function linkSimpleFINAccounts(accounts: LinkAccountMapping[]): Promise<void> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/bank/simplefin/link`, {
		method: 'POST',
		headers: await authHeaders(),
		body: JSON.stringify({ accounts })
	});
	if (!res.ok) {
		const err = await res.json().catch(() => ({}));
		throw new Error(err.error || `Failed to link accounts (${res.status})`);
	}
}

export async function disconnectSimpleFIN(unlinkAccounts = false): Promise<void> {
	const { apiBaseUrl } = getAppConfig();
	const qs = unlinkAccounts ? '?unlink_accounts=true' : '';
	const res = await fetch(`${apiBaseUrl}/api/v1/bank/simplefin${qs}`, {
		method: 'DELETE',
		headers: await authHeaders(false)
	});
	if (!res.ok) {
		const err = await res.json().catch(() => ({}));
		throw new Error(err.error || `Failed to disconnect SimpleFIN (${res.status})`);
	}
}

export async function triggerBankSyncNow(): Promise<TriggerBankSyncResponse> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/bank/sync-now`, {
		method: 'POST',
		headers: await authHeaders(false)
	});
	if (!res.ok) {
		const err = await res.json().catch(() => ({}));
		throw new Error(err.error || `Failed to trigger bank sync (${res.status})`);
	}
	return res.json();
}

export async function fetchBankSyncStatus(workflowId: string): Promise<BankSyncStatusResponse> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/bank/sync-status/${encodeURIComponent(workflowId)}`, {
		headers: await authHeaders(false)
	});
	if (!res.ok) {
		const err = await res.json().catch(() => ({}));
		throw new Error(err.error || `Failed to fetch bank sync status (${res.status})`);
	}
	return res.json();
}
