import { beforeEach, describe, expect, it, vi } from 'vitest';

const {
	fetchWorkspacesMock,
	createPersonalWorkspaceMock,
	clearCachesMock,
	demoLoadMock,
	demoStore
} = vi.hoisted(() => {
	const demoLoad = vi.fn().mockResolvedValue(undefined);
	return {
		fetchWorkspacesMock: vi.fn(),
		createPersonalWorkspaceMock: vi.fn(),
		clearCachesMock: vi.fn().mockResolvedValue(undefined),
		demoLoadMock: demoLoad,
		demoStore: {
			isDemo: false,
			dataVersion: 0,
			load: demoLoad
		}
	};
});

vi.mock('$app/environment', () => ({ browser: true }));
vi.mock('$lib/api/workspaces', () => ({
	fetchWorkspaces: (...args: unknown[]) => fetchWorkspacesMock(...args),
	createPersonalWorkspace: (...args: unknown[]) => createPersonalWorkspaceMock(...args)
}));
vi.mock('$lib/offline/db', () => ({
	BudgetLocalDB: class {
		clearCaches = clearCachesMock;
	}
}));
vi.mock('./demo.svelte', () => ({ demo: demoStore }));

import { PENDING_WORKSPACE_STORAGE_KEY } from '$lib/api/headers';
import { setPendingJoinedWorkspace, WorkspaceStore } from './workspace.svelte';

describe('WorkspaceStore', () => {
	beforeEach(() => {
		vi.clearAllMocks();
		localStorage.clear();
		demoStore.isDemo = false;
		demoStore.dataVersion = 0;
	});

	it('honours pending joined workspace after invite acceptance and switches cleanly', async () => {
		fetchWorkspacesMock.mockResolvedValue([
			{ id: 'ws-personal', name: 'Personal', role: 'owner', is_demo: false },
			{ id: 'ws-household', name: 'Household', role: 'member', is_demo: false }
		]);

		setPendingJoinedWorkspace('ws-household');
		const store = new WorkspaceStore();
		await store.load();

		expect(store.activeWorkspaceId).toBe('ws-household');
		expect(store.currentRole).toBe('member');
		expect(store.isOwner).toBe(false);
		expect(store.canEdit).toBe(true);
		expect(store.canCreatePersonalWorkspace).toBe(false);
		expect(localStorage.getItem(PENDING_WORKSPACE_STORAGE_KEY)).toBeNull();

		await store.switchWorkspace('ws-personal');
		expect(store.activeWorkspaceId).toBe('ws-personal');
		expect(store.isOwner).toBe(true);
		expect(clearCachesMock).toHaveBeenCalledTimes(1);
		expect(demoLoadMock).toHaveBeenCalledTimes(1);
		expect(demoStore.dataVersion).toBe(1);
	});

	it('allows creating personal workspace when user only belongs as a member', async () => {
		fetchWorkspacesMock.mockResolvedValue([
			{ id: 'ws-household', name: 'Household', role: 'read_only', is_demo: false }
		]);
		createPersonalWorkspaceMock.mockResolvedValue({
			id: 'ws-new-personal',
			name: 'My Budget',
			role: 'owner',
			is_demo: false
		});

		const store = new WorkspaceStore();
		await store.load();

		expect(store.isReadOnly).toBe(true);
		expect(store.canCreatePersonalWorkspace).toBe(true);

		await store.createPersonal('My Budget');
		expect(store.activeWorkspaceId).toBe('ws-new-personal');
		expect(store.isOwner).toBe(true);
		expect(store.canCreatePersonalWorkspace).toBe(false);
	});
});
