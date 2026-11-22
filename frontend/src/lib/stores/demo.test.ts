import { beforeEach, describe, expect, it, vi } from 'vitest';

const mockFetchSettings = vi.fn();
const mockToggleDemoMode = vi.fn();
const mockResetDemoMode = vi.fn();
const mockUpdateWorkspaceSettings = vi.fn();
const mockFetchSimulatorState = vi.fn();
const mockTriggerBankSyncNow = vi.fn();
const mockFetchBankSyncStatus = vi.fn();
const mockClearCaches = vi.fn();

vi.mock('$lib/api/settings', () => ({
	fetchSettings: (...args: unknown[]) => mockFetchSettings(...args),
	toggleDemoMode: (...args: unknown[]) => mockToggleDemoMode(...args),
	resetDemoMode: (...args: unknown[]) => mockResetDemoMode(...args),
	updateWorkspaceSettings: (...args: unknown[]) => mockUpdateWorkspaceSettings(...args),
	fetchSimulatorState: (...args: unknown[]) => mockFetchSimulatorState(...args)
}));

vi.mock('$lib/api/simplefin', () => ({
	triggerBankSyncNow: (...args: unknown[]) => mockTriggerBankSyncNow(...args),
	fetchBankSyncStatus: (...args: unknown[]) => mockFetchBankSyncStatus(...args)
}));

vi.mock('$lib/offline/db', () => ({
	BudgetLocalDB: class {
		clearCaches = mockClearCaches;
	}
}));

import { DemoStore } from './demo.svelte';

function makeSettings(isDemo: boolean) {
	return {
		workspace_id: isDemo ? 'ws-demo' : 'ws-real',
		workspace_name: isDemo ? 'Demo Sandbox Workspace' : 'Personal Workspace',
		is_demo: isDemo,
		demo_mode_enabled: isDemo,
		inbound_receipt_alias: isDemo ? 'demo@receipts.test' : 'w@receipts.test',
		simplefin: {
			connected: isDemo,
			is_demo_bridge: isDemo,
			last_synced_at: null,
			last_sync_warnings: []
		}
	};
}

describe('DemoStore', () => {
	let store: DemoStore;

	beforeEach(() => {
		vi.resetAllMocks();
		mockClearCaches.mockResolvedValue(undefined);
		mockFetchSimulatorState.mockResolvedValue({
			accounts: [],
			transactions: [],
			manual_transactions: [],
			fault: { mode: 'NONE', persistent: false, retry_after_seconds: 3, message: '' },
			unsynced_count: 2
		});

		store = new DemoStore();
		store.configurePolling(1, 5000);
	});

	it('toggles demo mode, clears IndexedDB cache, updates unsyncedCount and dataVersion', async () => {
		mockToggleDemoMode.mockResolvedValueOnce(makeSettings(true));
		await store.toggleDemo(true);

		expect(store.isDemo).toBe(true);
		expect(store.workspaceId).toBe('ws-demo');
		expect(store.unsyncedCount).toBe(2);
		expect(store.dataVersion).toBe(1);
		expect(mockClearCaches).toHaveBeenCalledTimes(1);

		mockToggleDemoMode.mockResolvedValueOnce(makeSettings(false));
		await store.toggleDemo(false);

		expect(store.isDemo).toBe(false);
		expect(store.unsyncedCount).toBe(0);
		expect(store.dataVersion).toBe(2);
		expect(mockClearCaches).toHaveBeenCalledTimes(2);
	});

	it('resets demo mode, clears cache, and increments dataVersion', async () => {
		mockResetDemoMode.mockResolvedValueOnce(makeSettings(true));
		await store.resetDemo();

		expect(store.isDemo).toBe(true);
		expect(store.dataVersion).toBe(1);
		expect(mockClearCaches).toHaveBeenCalledTimes(1);
	});

	it('polls Temporal sync status through RUNNING -> RETRYING -> COMPLETED', async () => {
		mockTriggerBankSyncNow.mockResolvedValueOnce({ workflow_id: 'wf-123', status: 'accepted' });
		mockFetchBankSyncStatus
			.mockResolvedValueOnce({ workflow_id: 'wf-123', status: 'RUNNING' })
			.mockResolvedValueOnce({
				workflow_id: 'wf-123',
				status: 'RETRYING',
				attempt: 2,
				last_error: 'simplefin rate limited; retry after 3s'
			})
			.mockResolvedValueOnce({
				workflow_id: 'wf-123',
				status: 'COMPLETED',
				result: {
					fetched_count: 3,
					inserted_count: 2,
					settled_count: 1,
					manual_matched_count: 0,
					transfers_linked: 0,
					normalized_count: 2
				}
			});
		mockFetchSettings.mockResolvedValueOnce(makeSettings(true));

		const finalStatus = await store.triggerSyncAndPoll();

		expect(finalStatus?.status).toBe('COMPLETED');
		expect(store.syncStatus).toBe('completed');
		expect(store.syncMessage).toContain('Synced 2 new, 1 settled');
		expect(store.dataVersion).toBe(1);
	});

	it('handles FAILED sync workflow cleanly', async () => {
		mockTriggerBankSyncNow.mockResolvedValueOnce({ workflow_id: 'wf-403', status: 'accepted' });
		mockFetchBankSyncStatus.mockResolvedValueOnce({
			workflow_id: 'wf-403',
			status: 'FAILED',
			last_error: 'simplefin auth error (403)'
		});
		mockFetchSettings.mockResolvedValueOnce(makeSettings(true));

		const finalStatus = await store.triggerSyncAndPoll();

		expect(finalStatus?.status).toBe('FAILED');
		expect(store.syncStatus).toBe('failed');
		expect(store.lastSyncError).toBe('simplefin auth error (403)');
		expect(store.dataVersion).toBe(1);
	});
});
