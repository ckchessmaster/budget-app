import { browser } from '$app/environment';
import { auth } from '$lib/stores/auth.svelte';

export const ACTIVE_WORKSPACE_STORAGE_KEY = 'budget_active_workspace_id';
export const PENDING_WORKSPACE_STORAGE_KEY = 'budget_pending_workspace_id';

let workspaceIdProvider: (() => string) | null = null;

/**
 * Registers an in-memory active workspace provider (called by WorkspaceStore) so API
 * helpers attach X-Workspace-ID without introducing circular imports.
 */
export function registerActiveWorkspaceProvider(provider: () => string): void {
	workspaceIdProvider = provider;
}

export function getActiveWorkspaceId(): string {
	if (workspaceIdProvider) {
		const provided = workspaceIdProvider().trim();
		if (provided) return provided;
	}
	if (browser && typeof window !== 'undefined' && window.localStorage) {
		return (window.localStorage.getItem(ACTIVE_WORKSPACE_STORAGE_KEY) || '').trim();
	}
	return '';
}

export interface AuthHeaderOptions {
	includeJson?: boolean;
	includeWorkspace?: boolean;
}

export async function buildAuthHeaders(opts: AuthHeaderOptions = {}): Promise<HeadersInit> {
	const { includeJson = true, includeWorkspace = true } = opts;
	const headers: Record<string, string> = {};
	if (includeJson) {
		headers['Content-Type'] = 'application/json';
	}
	const token = await auth.getValidToken();
	if (token) {
		headers['Authorization'] = `Bearer ${token}`;
	}
	if (includeWorkspace) {
		const workspaceId = getActiveWorkspaceId();
		if (workspaceId) {
			headers['X-Workspace-ID'] = workspaceId;
		}
	}
	return headers;
}
