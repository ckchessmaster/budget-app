import { getAppConfig } from '$lib/config';
import { auth } from '$lib/stores/auth.svelte';

export interface SimpleFINConnectionStatus {
	connected: boolean;
	is_demo_bridge: boolean;
	last_synced_at: string | null;
	last_sync_warnings: string[];
}

export interface SettingsResponse {
	workspace_id: string;
	workspace_name: string;
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
	const headers: Record<string, string> = {
		'Content-Type': 'application/json'
	};
	const token = await auth.getValidToken();
	if (token) {
		headers['Authorization'] = `Bearer ${token}`;
	}
	return headers;
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

export async function resetDemoMode(): Promise<SettingsResponse> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/demo/reset`, {
		method: 'POST',
		headers: await authHeaders()
	});
	if (!res.ok) {
		throw new Error(await parseError(res, 'Failed to reset demo mode'));
	}
	return res.json();
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
