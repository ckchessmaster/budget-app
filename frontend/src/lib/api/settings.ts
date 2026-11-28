import { getAppConfig } from '$lib/config';
import { fetchWorkflowStatus, WorkflowStatusError, type WorkflowStatus } from '$lib/api/simplefin';
import { buildAuthHeaders } from '$lib/api/headers';

export interface SimpleFINConnectionStatus {
	connected: boolean;
	is_demo_bridge: boolean;
	last_synced_at: string | null;
	last_sync_warnings: string[];
}

export interface SettingsResponse {
	workspace_id: string;
	workspace_name: string;
	role?: 'owner' | 'member' | 'read_only';
	is_demo: boolean;
	demo_mode_enabled: boolean;
	inbound_receipt_alias: string;
	simplefin: SimpleFINConnectionStatus;
}

export interface SimAccount {
	id: string;
	external_account_id: string;
	name: string;
	currency: string;
	balance: string;
	account_type: string;
	card_mode: string;
}

export interface SimTransaction {
	id: string;
	external_account_id: string;
	external_transaction_id: string;
	posted_at: string;
	amount: string;
	description: string;
	pending: boolean;
	is_synced: boolean;
}

export interface SimManualTransaction {
	id: string;
	account_id: string;
	account_name: string;
	external_account_id: string | null;
	amount: string;
	merchant_raw: string;
	transaction_date: string;
	status: string;
}

export type FaultMode = 'NONE' | 'RATE_LIMIT_429' | 'PARTIAL_WARNING' | 'AUTH_ERROR_403';

export interface SimFaultState {
	mode: FaultMode;
	persistent: boolean;
	retry_after_seconds: number;
	message: string;
}

export interface SimulatorStateResponse {
	accounts: SimAccount[];
	transactions: SimTransaction[];
	manual_transactions: SimManualTransaction[];
	fault: SimFaultState;
	unsynced_count: number;
}

export interface SimActionResponse {
	workflow_id?: string;
	sync_error?: string;
	external_transaction_id?: string;
}

async function authHeaders(): Promise<HeadersInit> {
	return buildAuthHeaders();
}

async function parseError(res: Response, fallback: string): Promise<string> {
	const body = await res.json().catch(() => ({}));
	return body.error || `${fallback} (${res.status})`;
}

export async function fetchSettings(): Promise<SettingsResponse> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/settings`, {
		headers: await authHeaders()
	});
	if (!res.ok) {
		throw new Error(await parseError(res, 'Failed to load settings'));
	}
	return res.json();
}

export async function updateWorkspaceSettings(name: string): Promise<SettingsResponse> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/settings/workspace`, {
		method: 'PATCH',
		headers: await authHeaders(),
		body: JSON.stringify({ name })
	});
	if (!res.ok) {
		throw new Error(await parseError(res, 'Failed to update workspace'));
	}
	return res.json();
}

export async function toggleDemoMode(enabled: boolean): Promise<SettingsResponse> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/demo/toggle`, {
		method: 'POST',
		headers: await authHeaders(),
		body: JSON.stringify({ enabled })
	});
	if (!res.ok) {
		throw new Error(await parseError(res, 'Failed to toggle demo mode'));
	}
	return res.json();
}

const DEMO_RESET_POLL_INTERVAL_MS = 1000;
const DEMO_RESET_TIMEOUT_MS = 240_000;

export interface DemoResetAccepted {
	workflow_id: string;
	status: string;
}

export interface DemoResetResult {
	workspace_id: string;
	categories_seeded: number;
	accounts_seeded: number;
	connection_ready: boolean;
	syncs_cancelled?: number;
}

/**
 * Tear the demo sandbox down and rebuild it, resolving once the new one exists.
 *
 * Reset used to be one long request that either finished or left the sandbox half
 * rebuilt — a deploy, a cancelled tab or an eviction could all land mid-flight. It now
 * starts a Temporal workflow and this function polls it, so the caller still gets a
 * settings response describing the finished sandbox while the work survives the tab
 * closing. A second reset while one is running is refused with 409 rather than queued.
 */
export async function resetDemoMode(triggerSync = false): Promise<SettingsResponse> {
	const started = await startDemoReset(triggerSync);
	await waitForDemoReset(started.workflow_id);
	return fetchSettings();
}

async function startDemoReset(triggerSync: boolean): Promise<DemoResetAccepted> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/demo/reset`, {
		method: 'POST',
		headers: await authHeaders(),
		body: JSON.stringify({ trigger_sync: triggerSync })
	});
	if (res.status === 409) {
		throw new Error('A demo reset is already running. Wait for it to finish and try again.');
	}
	if (!res.ok) {
		throw new Error(await parseError(res, 'Failed to reset demo mode'));
	}
	return res.json();
}

async function waitForDemoReset(workflowId: string): Promise<DemoResetResult | undefined> {
	const deadline = Date.now() + DEMO_RESET_TIMEOUT_MS;
	while (Date.now() < deadline) {
		let status: WorkflowStatus<DemoResetResult>;
		try {
			status = await fetchWorkflowStatus<DemoResetResult>(workflowId);
		} catch (err) {
			// A reset deletes the sandbox named in its own workflow ID and seeds the
			// replacement under a derived one, so there is a window — one activity hop, or
			// a seed retrying with backoff — in which the caller has no demo workspace at
			// all and the status endpoint answers 404. The workflow is still running and it
			// survives this tab closing, so keep polling instead of reporting a failure that
			// resolves itself on the next tick.
			if (err instanceof WorkflowStatusError && err.status === 404) {
				await sleep(DEMO_RESET_POLL_INTERVAL_MS);
				continue;
			}
			throw err;
		}
		if (status.status === 'COMPLETED') {
			return status.result;
		}
		if (status.status === 'FAILED') {
			throw new Error(status.last_error || 'Demo reset failed');
		}
		await sleep(DEMO_RESET_POLL_INTERVAL_MS);
	}
	throw new Error('Timed out waiting for the demo sandbox to rebuild');
}

function sleep(ms: number): Promise<void> {
	return new Promise((resolve) => setTimeout(resolve, ms));
}

export async function fetchSimulatorState(): Promise<SimulatorStateResponse> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/demo/simulator`, {
		headers: await authHeaders()
	});
	if (!res.ok) {
		throw new Error(await parseError(res, 'Failed to load simulator state'));
	}
	return res.json();
}

export async function stageDemoTransaction(payload: {
	preset?: string;
	external_account_id?: string;
	description?: string;
	amount?: number;
	posted_at?: string;
	pending?: boolean;
	sync_now?: boolean;
}): Promise<SimActionResponse> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/demo/simulator/transaction`, {
		method: 'POST',
		headers: await authHeaders(),
		body: JSON.stringify(payload)
	});
	if (!res.ok) {
		throw new Error(await parseError(res, 'Failed to stage demo transaction'));
	}
	return res.json();
}

export async function settleDemoPending(payload: {
	demo_transaction_id: string;
	settled_amount?: number;
	tip_percent?: number;
	new_description?: string;
	append_suffix?: string;
	sync_now?: boolean;
}): Promise<SimActionResponse> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/demo/simulator/settle`, {
		method: 'POST',
		headers: await authHeaders(),
		body: JSON.stringify(payload)
	});
	if (!res.ok) {
		throw new Error(await parseError(res, 'Failed to settle pending transaction'));
	}
	return res.json();
}

export async function stageDemoTransfer(payload: {
	from_external_account_id: string;
	to_external_account_id: string;
	amount: number;
	day_offset?: number;
	description?: string;
	sync_now?: boolean;
}): Promise<SimActionResponse> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/demo/simulator/transfer`, {
		method: 'POST',
		headers: await authHeaders(),
		body: JSON.stringify(payload)
	});
	if (!res.ok) {
		throw new Error(await parseError(res, 'Failed to stage transfer'));
	}
	return res.json();
}

export async function stageDemoManualMatch(payload: {
	manual_transaction_id: string;
	tip_percent?: number;
	day_offset?: number;
	sync_now?: boolean;
}): Promise<SimActionResponse> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/demo/simulator/match-manual`, {
		method: 'POST',
		headers: await authHeaders(),
		body: JSON.stringify(payload)
	});
	if (!res.ok) {
		throw new Error(await parseError(res, 'Failed to stage manual match'));
	}
	return res.json();
}

export async function updateDemoFault(payload: SimFaultState): Promise<SimFaultState> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/demo/simulator/fault`, {
		method: 'PUT',
		headers: await authHeaders(),
		body: JSON.stringify(payload)
	});
	if (!res.ok) {
		throw new Error(await parseError(res, 'Failed to update fault config'));
	}
	return res.json();
}
