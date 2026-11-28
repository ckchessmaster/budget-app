import { getAppConfig } from '$lib/config';
import { buildAuthHeaders } from './headers';
import { ApiError } from './workspaces';

export interface SignupValidateResponse {
	valid: boolean;
	expires_at: string;
	email?: string;
}

export interface SignupRedeemRequest {
	token: string;
	email: string;
	name: string;
	username: string;
}

export interface SignupRedeemResponse {
	setup_url: string;
}

export type PlatformInviteStatus = 'ACTIVE' | 'REDEEMED' | 'REVOKED' | 'EXPIRED';

export interface PlatformInvitation {
	id: string;
	label: string;
	recipient_email?: string;
	status: PlatformInviteStatus;
	expires_at: string;
	redeemed_by_email?: string;
	redeemed_at?: string;
	created_at: string;
	invite_url?: string;
	email_dispatched?: boolean;
}

export interface CreatePlatformInviteRequest {
	label?: string;
	expires_in_hours?: number;
	email?: string;
}

export interface PlatformUser {
	user_id: string;
	email: string;
	display_name: string;
	is_active: boolean;
	last_login_at?: string;
	created_at: string;
	owned_workspace_count: number;
	member_workspace_count: number;
}

export interface AdminWorkspaceOverview {
	workspace_id: string;
	workspace_name: string;
	created_at: string;
	member_count: number;
	owner_email: string;
	simplefin_last_synced_at?: string;
	simplefin_status: 'OK' | 'ERROR' | 'PENDING' | 'NOT_CONFIGURED' | string;
	simplefin_error?: string;
}

export interface DependencyHealth {
	status: string;
	latency_ms: number;
}

export interface StaleOrErroredSimpleFIN {
	workspace_id: string;
	workspace_name: string;
	status: string;
	last_synced_at?: string;
	error_summary?: string;
}

export interface AdminHealthResponse {
	status: 'ok' | 'degraded' | string;
	checked_at: string;
	dependencies: Record<string, DependencyHealth>;
	simplefin: {
		configured_workspaces: number;
		stale_or_errored: StaleOrErroredSimpleFIN[];
	};
	temporal_workflows_24h: {
		failed_bank_syncs: number;
		failed_receipt_workflows: number;
		failed_invite_emails?: number;
	};
	outbound_email_24h: {
		sent: number;
		failed: number;
	};
}

async function throwAdminApiError(res: Response, fallback: string): Promise<never> {
	const body = (await res.json().catch(() => ({}))) as { error?: string };
	const code = body.error || `${fallback} (${res.status})`;
	throw new ApiError(res.status, code, code);
}

export async function validatePlatformInvite(inviteToken: string): Promise<SignupValidateResponse> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(
		`${apiBaseUrl}/api/v1/signup/validate?invite=${encodeURIComponent(inviteToken)}`
	);
	if (!res.ok) {
		await throwAdminApiError(res, 'Invalid or expired invite');
	}
	return res.json();
}

export async function redeemPlatformInvite(
	payload: SignupRedeemRequest
): Promise<SignupRedeemResponse> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/signup/redeem`, {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify(payload)
	});
	if (!res.ok) {
		await throwAdminApiError(res, 'Failed to create account');
	}
	return res.json();
}

export async function fetchPlatformInvitations(): Promise<PlatformInvitation[]> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/admin/invitations`, {
		headers: await buildAuthHeaders({ includeJson: false, includeWorkspace: false })
	});
	if (!res.ok) {
		await throwAdminApiError(res, 'Failed to load invitations');
	}
	return res.json();
}

export async function createPlatformInvitation(
	payload: CreatePlatformInviteRequest
): Promise<PlatformInvitation> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/admin/invitations`, {
		method: 'POST',
		headers: await buildAuthHeaders({ includeWorkspace: false }),
		body: JSON.stringify(payload)
	});
	if (!res.ok) {
		await throwAdminApiError(res, 'Failed to generate invitation');
	}
	return res.json();
}

export async function revokePlatformInvitation(id: string): Promise<void> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/admin/invitations/${encodeURIComponent(id)}`, {
		method: 'DELETE',
		headers: await buildAuthHeaders({ includeJson: false, includeWorkspace: false })
	});
	if (!res.ok) {
		await throwAdminApiError(res, 'Failed to revoke invitation');
	}
}

export async function fetchPlatformUsers(): Promise<PlatformUser[]> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/admin/users`, {
		headers: await buildAuthHeaders({ includeJson: false, includeWorkspace: false })
	});
	if (!res.ok) {
		await throwAdminApiError(res, 'Failed to load platform users');
	}
	return res.json();
}

export async function updatePlatformUserStatus(
	userId: string,
	isActive: boolean
): Promise<PlatformUser> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(
		`${apiBaseUrl}/api/v1/admin/users/${encodeURIComponent(userId)}/status`,
		{
			method: 'PATCH',
			headers: await buildAuthHeaders({ includeWorkspace: false }),
			body: JSON.stringify({ is_active: isActive })
		}
	);
	if (!res.ok) {
		await throwAdminApiError(res, 'Failed to update user status');
	}
	return res.json();
}

export async function deletePlatformUser(userId: string): Promise<void> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/admin/users/${encodeURIComponent(userId)}`, {
		method: 'DELETE',
		headers: await buildAuthHeaders({ includeJson: false, includeWorkspace: false })
	});
	if (!res.ok) {
		await throwAdminApiError(res, 'Failed to delete user');
	}
}

export async function fetchAdminWorkspaces(): Promise<AdminWorkspaceOverview[]> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/admin/workspaces`, {
		headers: await buildAuthHeaders({ includeJson: false, includeWorkspace: false })
	});
	if (!res.ok) {
		await throwAdminApiError(res, 'Failed to load workspace metadata');
	}
	return res.json();
}

export async function fetchAdminHealth(): Promise<AdminHealthResponse> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/admin/health`, {
		headers: await buildAuthHeaders({ includeJson: false, includeWorkspace: false })
	});
	if (!res.ok) {
		await throwAdminApiError(res, 'Failed to load system health');
	}
	return res.json();
}
