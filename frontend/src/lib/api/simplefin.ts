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

export interface BankSyncStatusResponse {
	workflow_id: string;
	status: 'RUNNING' | 'RETRYING' | 'COMPLETED' | 'FAILED';
	attempt?: number;
	last_error?: string;
	result?: BankSyncResult;
}

async function authHeaders(): Promise<HeadersInit> {
	const headers: Record<string, string> = {
		'Content-Type': 'application/json'
	};
	const token = await auth.getValidToken();
	if (token) {
		headers['Authorization'] = `Bearer ${token}`;
	}
	return headers;
}

export async function claimSimpleFINToken(setupToken: string): Promise<SimpleFINDiscoveredAccount[]> {
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
	const data = await res.json();
	return Array.isArray(data.accounts) ? data.accounts : [];
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
		headers: await authHeaders()
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
		headers: await authHeaders()
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
		headers: await authHeaders()
	});
	if (!res.ok) {
		const err = await res.json().catch(() => ({}));
		throw new Error(err.error || `Failed to fetch bank sync status (${res.status})`);
	}
	return res.json();
}
