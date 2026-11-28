import { browser } from '$app/environment';
import {
	ACTIVE_WORKSPACE_STORAGE_KEY,
	PENDING_WORKSPACE_STORAGE_KEY,
	registerActiveWorkspaceProvider
} from '$lib/api/headers';
import {
	createPersonalWorkspace,
	fetchWorkspaces,
	type WorkspaceRole,
	type WorkspaceSummary
} from '$lib/api/workspaces';
import { BudgetLocalDB } from '$lib/offline/db';
import { demo } from './demo.svelte';

function readStorage(key: string): string {
	if (!browser || typeof window === 'undefined' || !window.localStorage) return '';
	return (window.localStorage.getItem(key) || '').trim();
}

function writeStorage(key: string, value: string): void {
	if (!browser || typeof window === 'undefined' || !window.localStorage) return;
	if (value) {
		window.localStorage.setItem(key, value);
	} else {
		window.localStorage.removeItem(key);
	}
}

export function setPendingJoinedWorkspace(workspaceId: string): void {
	writeStorage(PENDING_WORKSPACE_STORAGE_KEY, workspaceId);
}

export class WorkspaceStore {
	workspaces = $state<WorkspaceSummary[]>([]);
	activeWorkspaceId = $state('');
	loaded = $state(false);
	loading = $state(false);
	version = $state(0);

	constructor() {
		registerActiveWorkspaceProvider(() => this.activeWorkspaceId);
		if (browser) {
			this.activeWorkspaceId = readStorage(ACTIVE_WORKSPACE_STORAGE_KEY);
		}
	}

	get activeWorkspace(): WorkspaceSummary | null {
		return this.workspaces.find((w) => w.id === this.activeWorkspaceId) ?? this.workspaces[0] ?? null;
	}

	get currentRole(): WorkspaceRole {
		if (demo.isDemo) return 'owner';
		return this.activeWorkspace?.role ?? 'owner';
	}

	get isOwner(): boolean {
		return this.currentRole === 'owner';
	}

	get canEdit(): boolean {
		return this.currentRole === 'owner' || this.currentRole === 'member';
	}

	get isReadOnly(): boolean {
		return this.currentRole === 'read_only';
	}

	get canCreatePersonalWorkspace(): boolean {
		if (!this.loaded) return false;
		return !this.workspaces.some((w) => !w.is_demo && w.role === 'owner');
	}

	async load(): Promise<void> {
		this.loading = true;
		try {
			const list = await fetchWorkspaces();
			this.workspaces = Array.isArray(list) ? list : [];

			const pendingId = readStorage(PENDING_WORKSPACE_STORAGE_KEY);
			const savedId = readStorage(ACTIVE_WORKSPACE_STORAGE_KEY);

			let chosen = '';
			if (pendingId && this.workspaces.some((w) => w.id === pendingId)) {
				chosen = pendingId;
				writeStorage(PENDING_WORKSPACE_STORAGE_KEY, '');
			} else if (this.activeWorkspaceId && this.workspaces.some((w) => w.id === this.activeWorkspaceId)) {
				chosen = this.activeWorkspaceId;
			} else if (savedId && this.workspaces.some((w) => w.id === savedId)) {
				chosen = savedId;
			} else {
				const owned = this.workspaces.find((w) => w.role === 'owner');
				chosen = owned?.id ?? this.workspaces[0]?.id ?? '';
			}

			const changed = chosen !== this.activeWorkspaceId;
			this.activeWorkspaceId = chosen;
			writeStorage(ACTIVE_WORKSPACE_STORAGE_KEY, chosen);
			this.loaded = true;
			if (changed) {
				this.version += 1;
			}
		} finally {
			this.loading = false;
		}
	}

	async switchWorkspace(id: string): Promise<void> {
		if (!id || id === this.activeWorkspaceId) return;
		if (!this.workspaces.some((w) => w.id === id)) return;

		this.activeWorkspaceId = id;
		writeStorage(ACTIVE_WORKSPACE_STORAGE_KEY, id);

		// Clear read-only offline caches so stale budget/account rows from another workspace
		// never flash while switching, while leaving workspace-tagged offline queue items intact.
		await new BudgetLocalDB().clearCaches().catch(() => {});

		await demo.load().catch(() => {});
		this.version += 1;
		demo.dataVersion += 1;
	}

	async createPersonal(name: string): Promise<WorkspaceSummary> {
		const created = await createPersonalWorkspace(name);
		this.workspaces = [created, ...this.workspaces.filter((w) => w.id !== created.id)];
		this.activeWorkspaceId = created.id;
		writeStorage(ACTIVE_WORKSPACE_STORAGE_KEY, created.id);
		await new BudgetLocalDB().clearCaches().catch(() => {});
		await demo.load().catch(() => {});
		this.version += 1;
		demo.dataVersion += 1;
		return created;
	}

	updateWorkspaceNameLocally(id: string, name: string): void {
		this.workspaces = this.workspaces.map((w) => (w.id === id ? { ...w, name } : w));
	}

	resetOnLogout(): void {
		this.workspaces = [];
		this.activeWorkspaceId = '';
		this.loaded = false;
		this.loading = false;
		this.version = 0;
		writeStorage(ACTIVE_WORKSPACE_STORAGE_KEY, '');
	}
}

export const workspace = new WorkspaceStore();
