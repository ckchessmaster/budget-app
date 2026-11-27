import {
	fetchSettings,
	fetchSimulatorState,
	resetDemoMode,
	toggleDemoMode,
	updateWorkspaceSettings,
	type SettingsResponse,
	type SimpleFINConnectionStatus
} from '$lib/api/settings';
import {
	fetchBankSyncStatus,
	triggerBankSyncNow,
	type BankSyncResult,
	type BankSyncStatusResponse
} from '$lib/api/simplefin';
import { BudgetLocalDB } from '$lib/offline/db';

export type SyncProgressStatus = 'idle' | 'running' | 'retrying' | 'completed' | 'failed';

const POLL_INTERVAL_MS = 800;
const POLL_TIMEOUT_MS = 90000;

function sleep(ms: number): Promise<void> {
	return new Promise((resolve) => setTimeout(resolve, ms));
}

/**
 * Turn a sync failure into something actionable.
 *
 * An expired session used to surface here as the API's bare error code, so the banner
 * read `invalid_token` next to the demo simulator and looked like the bank feed was
 * broken rather than like the user needed to sign in again.
 */
function describeSyncError(err: unknown): string {
	const message = err instanceof Error ? err.message : String(err ?? '');
	if (message === 'invalid_token' || message === 'missing_token') {
		return 'Session expired — sign in again to sync';
	}
	return message || 'Failed to sync bank feed';
}

export class DemoStore {
	loaded = $state(false);
	loading = $state(false);
	isDemo = $state(false);
	workspaceId = $state('');
	workspaceName = $state('');
	inboundReceiptAlias = $state('');
	simplefin = $state<SimpleFINConnectionStatus>({
		connected: false,
		is_demo_bridge: false,
		last_synced_at: null,
		last_sync_warnings: []
	});

	unsyncedCount = $state(0);
	drawerOpen = $state(false);
	dataVersion = $state(0);

	syncStatus = $state<SyncProgressStatus>('idle');
	syncAttempt = $state(0);
	syncMessage = $state('');
	lastSyncResult = $state<BankSyncResult | null>(null);
	lastSyncError = $state<string | null>(null);

	private pollIntervalMs = POLL_INTERVAL_MS;
	private pollTimeoutMs = POLL_TIMEOUT_MS;

	configurePolling(intervalMs: number, timeoutMs: number) {
		this.pollIntervalMs = intervalMs;
		this.pollTimeoutMs = timeoutMs;
	}

	private applySettings(s: SettingsResponse) {
		this.workspaceId = s.workspace_id;
		this.workspaceName = s.workspace_name;
		this.isDemo = s.is_demo;
		this.inboundReceiptAlias = s.inbound_receipt_alias;
		this.simplefin = s.simplefin;
		this.loaded = true;
	}

	async load(): Promise<void> {
		this.loading = true;
		try {
			const s = await fetchSettings();
			this.applySettings(s);
			if (s.is_demo) {
				await this.refreshSimulatorCount();
			} else {
				this.unsyncedCount = 0;
			}
		} finally {
			this.loading = false;
		}
	}

	async toggleDemo(enabled: boolean): Promise<void> {
		this.loading = true;
		const prevWorkspaceId = this.workspaceId;
		try {
			const s = await toggleDemoMode(enabled);
			const localDB = new BudgetLocalDB();
			await localDB.clearCaches().catch(() => {});
			if (prevWorkspaceId && typeof localDB.clearQueuedForWorkspace === 'function') {
				await localDB.clearQueuedForWorkspace(prevWorkspaceId).catch(() => {});
			}
			this.applySettings(s);
			this.syncStatus = 'idle';
			this.syncMessage = '';
			this.lastSyncResult = null;
			this.lastSyncError = null;
			if (!enabled) {
				this.drawerOpen = false;
				this.unsyncedCount = 0;
			} else {
				await this.refreshSimulatorCount();
			}
			this.dataVersion += 1;
		} finally {
			this.loading = false;
		}
	}

	async resetDemo(): Promise<void> {
		this.loading = true;
		const prevWorkspaceId = this.workspaceId;
		try {
			const s = await resetDemoMode();
			const localDB = new BudgetLocalDB();
			await localDB.clearCaches().catch(() => {});
			if (prevWorkspaceId && typeof localDB.clearQueuedForWorkspace === 'function') {
				await localDB.clearQueuedForWorkspace(prevWorkspaceId).catch(() => {});
			}
			this.applySettings(s);
			this.syncStatus = 'idle';
			this.syncMessage = 'Demo sandbox reset to initial snapshot.';
			this.lastSyncResult = null;
			this.lastSyncError = null;
			await this.refreshSimulatorCount();
			this.dataVersion += 1;
		} finally {
			this.loading = false;
		}
	}

	async updateWorkspaceName(name: string): Promise<void> {
		const s = await updateWorkspaceSettings(name);
		this.applySettings(s);
	}

	async refreshSimulatorCount(): Promise<void> {
		if (!this.isDemo) {
			this.unsyncedCount = 0;
			return;
		}
		try {
			const sim = await fetchSimulatorState();
			this.unsyncedCount = sim.unsynced_count;
		} catch {
			// Ignore transient simulator count errors
		}
	}

	async triggerSyncAndPoll(existingWorkflowId?: string): Promise<BankSyncStatusResponse | null> {
		this.syncStatus = 'running';
		this.syncAttempt = 1;
		this.syncMessage = 'Starting Temporal bank sync...';
		this.lastSyncResult = null;
		this.lastSyncError = null;

		try {
			let workflowId = existingWorkflowId;
			if (!workflowId) {
				const started = await triggerBankSyncNow();
				workflowId = started.workflow_id;
			}
			if (!workflowId) {
				throw new Error('Missing workflow_id from sync trigger');
			}

			const deadline = Date.now() + this.pollTimeoutMs;
			while (Date.now() < deadline) {
				const st = await fetchBankSyncStatus(workflowId);
				if (st.status === 'RUNNING') {
					this.syncStatus = 'running';
					this.syncMessage = 'Syncing via Temporal BankSyncWorkflow...';
				} else if (st.status === 'RETRYING') {
					this.syncStatus = 'retrying';
					this.syncAttempt = st.attempt ?? 2;
					this.syncMessage = st.last_error
						? `Retrying (attempt ${this.syncAttempt}): ${st.last_error}`
						: `Retrying (attempt ${this.syncAttempt})...`;
				} else if (st.status === 'COMPLETED') {
					this.syncStatus = 'completed';
					this.lastSyncResult = st.result ?? null;
					this.syncMessage = st.result
						? `Synced ${st.result.inserted_count} new, ${st.result.settled_count} settled, ${st.result.manual_matched_count} manual matched, ${st.result.transfers_linked} transfers.`
						: 'Bank sync completed.';
					await this.load().catch(() => {});
					this.dataVersion += 1;
					return st;
				} else if (st.status === 'FAILED') {
					this.syncStatus = 'failed';
					this.lastSyncError = st.last_error || 'Bank sync workflow failed';
					this.syncMessage = this.lastSyncError;
					await this.load().catch(() => {});
					this.dataVersion += 1;
					return st;
				}

				await sleep(this.pollIntervalMs);
			}

			this.syncStatus = 'failed';
			this.lastSyncError = 'Sync status polling timed out';
			this.syncMessage = this.lastSyncError;
			return null;
		} catch (err) {
			this.syncStatus = 'failed';
			this.lastSyncError = describeSyncError(err);
			this.syncMessage = this.lastSyncError;
			return null;
		}
	}

	openDrawer() {
		this.drawerOpen = true;
	}

	closeDrawer() {
		this.drawerOpen = false;
	}

	resetOnLogout() {
		this.loaded = false;
		this.isDemo = false;
		this.workspaceId = '';
		this.workspaceName = '';
		this.inboundReceiptAlias = '';
		this.unsyncedCount = 0;
		this.drawerOpen = false;
		this.syncStatus = 'idle';
		this.syncMessage = '';
		this.lastSyncResult = null;
		this.lastSyncError = null;
	}
}

export const demo = new DemoStore();
