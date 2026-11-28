import { getAppConfig } from '$lib/config';
import { auth } from '$lib/stores/auth.svelte';
import { buildAuthHeaders } from './headers';

export type WorkspaceRole = 'owner' | 'member' | 'read_only';

export interface WorkspaceSummary {
	id: string;
	name: string;
	role: WorkspaceRole;
	is_demo: boolean;
}

export interface WorkspaceMember {
	user_id: string;
	email: string;
	display_name: string;
	role: WorkspaceRole;
	created_at: string;
	is_current_user: boolean;
}

export interface WorkspaceInvitation {
	id: string;
	workspace_id: string;
	email: string;
	role: 'member' | 'read_only';
	status: 'PENDING' | 'ACCEPTED' | 'REVOKED' | 'EXPIRED';
	expires_at: string;
	created_at: string;
	invited_by_email?: string;
}

export interface InvitePreviewResponse {
	workspace_name: string;
	email: string;
	invited_by_email?: string;
	role: 'member' | 'read_only';
	status: 'PENDING' | 'ACCEPTED' | 'REVOKED' | 'EXPIRED';
	expires_at: string;
	email_mismatch: boolean;
	signed_in_email?: string;
}

export interface AcceptInviteResponse {
	workspace_id: string;
	workspace_name: string;
	role: 'member' | 'read_only';
	is_new_user: boolean;
	redirect_url: string;
}

export class ApiError extends Error {
	status: number;
	code: string;

	constructor(status: number, code: string, message?: string) {
		super(message || code);
		this.name = 'ApiError';
		this.status = status;
		this.code = code;
	}
}

async function throwApiError(res: Response, fallback: string): Promise<never> {
	const body = (await res.json().catch(() => ({}))) as { error?: string };
	const code = body.error || `${fallback} (${res.status})`;
	throw new ApiError(res.status, code, code);
}

export async function fetchWorkspaces(): Promise<WorkspaceSummary[]> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/workspaces`, {
		headers: await buildAuthHeaders({ includeJson: false, includeWorkspace: false })
	});
	if (!res.ok) {
		await throwApiError(res, 'Failed to load workspaces');
	}
	return res.json();
}

export async function createPersonalWorkspace(name: string): Promise<WorkspaceSummary> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/workspaces`, {
		method: 'POST',
		headers: await buildAuthHeaders({ includeWorkspace: false }),
		body: JSON.stringify({ name })
	});
	if (!res.ok) {
		await throwApiError(res, 'Failed to create workspace');
	}
	return res.json();
}

export async function fetchWorkspaceMembers(): Promise<WorkspaceMember[]> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/workspace/members`, {
		headers: await buildAuthHeaders({ includeJson: false })
	});
	if (!res.ok) {
		await throwApiError(res, 'Failed to load workspace members');
	}
	return res.json();
}

export async function updateMemberRole(
	userId: string,
	role: 'member' | 'read_only'
): Promise<WorkspaceMember> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/workspace/members/${encodeURIComponent(userId)}`, {
		method: 'PATCH',
		headers: await buildAuthHeaders(),
		body: JSON.stringify({ role })
	});
	if (!res.ok) {
		await throwApiError(res, 'Failed to update member role');
	}
	return res.json();
}

export async function removeWorkspaceMember(userId: string): Promise<void> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/workspace/members/${encodeURIComponent(userId)}`, {
		method: 'DELETE',
		headers: await buildAuthHeaders({ includeJson: false })
	});
	if (!res.ok) {
		await throwApiError(res, 'Failed to remove workspace member');
	}
}

export async function fetchWorkspaceInvitations(): Promise<WorkspaceInvitation[]> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/workspace/invitations`, {
		headers: await buildAuthHeaders({ includeJson: false })
	});
	if (!res.ok) {
		await throwApiError(res, 'Failed to load invitations');
	}
	return res.json();
}

export async function createWorkspaceInvitation(
	email: string,
	role: 'member' | 'read_only'
): Promise<WorkspaceInvitation> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/workspace/invitations`, {
		method: 'POST',
		headers: await buildAuthHeaders(),
		body: JSON.stringify({ email, role })
	});
	if (!res.ok) {
		await throwApiError(res, 'Failed to send invitation');
	}
	return res.json();
}

export async function resendWorkspaceInvitation(id: string): Promise<WorkspaceInvitation> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(
		`${apiBaseUrl}/api/v1/workspace/invitations/${encodeURIComponent(id)}/resend`,
		{
			method: 'POST',
			headers: await buildAuthHeaders({ includeJson: false })
		}
	);
	if (!res.ok) {
		await throwApiError(res, 'Failed to resend invitation');
	}
	return res.json();
}

export async function revokeWorkspaceInvitation(id: string): Promise<void> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/workspace/invitations/${encodeURIComponent(id)}`, {
		method: 'DELETE',
		headers: await buildAuthHeaders({ includeJson: false })
	});
	if (!res.ok) {
		await throwApiError(res, 'Failed to revoke invitation');
	}
}

export async function previewInvitation(token: string): Promise<InvitePreviewResponse> {
	const { apiBaseUrl } = getAppConfig();
	const headers: Record<string, string> = {};
	const bearer = await auth.getValidToken().catch(() => null);
	if (bearer) {
		headers['Authorization'] = `Bearer ${bearer}`;
	}
	const res = await fetch(
		`${apiBaseUrl}/api/v1/invitations/preview?token=${encodeURIComponent(token)}`,
		{ headers }
	);
	if (!res.ok) {
		await throwApiError(res, 'Failed to load invitation');
	}
	return res.json();
}

export async function acceptInvitation(token: string, name: string): Promise<AcceptInviteResponse> {
	const { apiBaseUrl } = getAppConfig();
	const headers: Record<string, string> = {
		'Content-Type': 'application/json'
	};
	const bearer = await auth.getValidToken().catch(() => null);
	if (bearer) {
		headers['Authorization'] = `Bearer ${bearer}`;
	}
	const res = await fetch(`${apiBaseUrl}/api/v1/invitations/accept`, {
		method: 'POST',
		headers,
		body: JSON.stringify({ token, name })
	});
	if (!res.ok) {
		await throwApiError(res, 'Failed to accept invitation');
	}
	return res.json();
}
