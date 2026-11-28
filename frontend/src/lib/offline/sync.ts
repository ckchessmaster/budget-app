import { getAppConfig } from '$lib/config';
import { getActiveWorkspaceId } from '$lib/api/headers';
import { auth } from '$lib/stores/auth.svelte';
import { BudgetLocalDB } from './db';

export const SYNC_TAG = 'sync-transactions';

/**
 * A replay batch is a handful of single-row inserts, so it normally resolves on the first
 * poll. The ceiling exists for the case where the worker is backed up: leaving the queue
 * intact is always the right answer, because the server dedupes on client_id.
 */
const DEFAULT_POLL_INTERVAL_MS = 1000;
const DEFAULT_POLL_TIMEOUT_MS = 60_000;

export interface SyncFlushOptions {
	pollIntervalMs?: number;
	pollTimeoutMs?: number;
	workspaceId?: string;
}

export interface ReplayRejection {
	localId: string;
	reason: string;
	message?: string;
	/** True when the server gave up on a transient condition: worth sending again. */
	retryable: boolean;
}

export interface SyncFlushResult {
	syncedCount: number;
	/** Items the server rejected for a reason the user has to fix. */
	failedCount: number;
	/** Items still queued, either because the batch never resolved or the server asked for a resend. */
	queuedCount: number;
	workflowId?: string;
	rejections: ReplayRejection[];
}

interface RejectedItem {
	client_id: string;
	reason?: string;
	message?: string;
	retryable?: boolean;
}

interface ReplayResult {
	applied?: string[];
	rejected?: RejectedItem[];
	warnings?: string[];
}

interface WorkflowStatus {
	workflow_id: string;
	status: 'RUNNING' | 'RETRYING' | 'COMPLETED' | 'FAILED';
	result?: ReplayResult;
	last_error?: string;
	error?: string;
}

function sleep(ms: number): Promise<void> {
	return new Promise((resolve) => setTimeout(resolve, ms));
}

function emptyResult(workflowId?: string): SyncFlushResult {
	return { syncedCount: 0, failedCount: 0, queuedCount: 0, workflowId, rejections: [] };
}

/**
 * Hands the queued batch to the durable replay workflow and drains the local queue from its
 * verdicts.
 *
 * The request no longer waits for the rows to exist. POST /api/v1/transactions/sync answers
 * 202 with a workflow id, and every item is settled here by its client_id: applied means
 * delete it from the queue, terminally rejected means keep it and show why, retryable means
 * keep it and try again later. Nothing is ever dropped because a poll ran out of patience —
 * resending an already-applied item is a no-op on the server, which is the entire point of
 * moving this into a workflow.
 */
export async function flushOfflineQueue(
	db: BudgetLocalDB = new BudgetLocalDB(),
	apiBaseUrl?: string,
	token?: string | null,
	options: SyncFlushOptions = {}
): Promise<SyncFlushResult> {
	const pending = await db.getPendingQueue(options.workspaceId);
	if (pending.length === 0) {
		return emptyResult();
	}

	const baseUrl = apiBaseUrl ?? getAppConfig().apiBaseUrl;
	const authToken = token !== undefined ? token : await auth.getValidToken();
	if (token === undefined && !authToken) {
		// Session is unavailable (e.g. offline or awaiting re-authentication). Keep the queue
		// untouched rather than firing an unauthenticated request that marks every item FAILED.
		return { ...emptyResult(), queuedCount: pending.length };
	}

	const headers: Record<string, string> = {
		'Content-Type': 'application/json'
	};
	if (authToken) {
		headers['Authorization'] = `Bearer ${authToken}`;
	}
	const wsId = options.workspaceId || getActiveWorkspaceId();
	if (wsId) {
		headers['X-Workspace-ID'] = wsId;
	}

	for (const item of pending) {
		await db.updateSyncStatus(item.localId, 'SYNCING');
	}

	let res: Response;
	try {
		res = await fetch(`${baseUrl}/api/v1/transactions/sync`, {
			method: 'POST',
			headers,
			body: JSON.stringify({
				items: pending.map((item) => ({
					client_id: item.localId,
					account_id: item.accountId,
					amount: item.amount,
					merchant_raw: item.merchantRaw,
					category_id: item.categoryId,
					transaction_date: item.transactionDate,
					user_notes: item.userNotes ?? ''
				}))
			})
		});
	} catch (err) {
		// Network failures (offline / lie-fi) are transient: keep items QUEUED for automatic retry.
		for (const item of pending) {
			await db.updateSyncStatus(item.localId, 'QUEUED');
		}
		throw err;
	}

	if (res.status !== 202) {
		const isTransientEdgeFailure =
			res.status === 401 || res.status === 429 || res.status >= 500;
		for (const item of pending) {
			await db.updateSyncStatus(item.localId, isTransientEdgeFailure ? 'QUEUED' : 'FAILED');
		}
		throw new Error(`Offline sync failed (${res.status})`);
	}

	const accepted = (await res.json().catch(() => ({}))) as { workflow_id?: string };
	if (!accepted.workflow_id) {
		for (const item of pending) {
			await db.updateSyncStatus(item.localId, 'QUEUED');
		}
		throw new Error('Offline sync accepted no batch');
	}

	const workflowId = accepted.workflow_id;
	const status = await pollWorkflowStatus(baseUrl, headers, workflowId, options);

	if (!status || status.status === 'RUNNING' || status.status === 'RETRYING') {
		// Timed out, or the status endpoint is unreachable. The batch may still be
		// running, so the queue goes back to QUEUED rather than FAILED: the next flush
		// resends it and the server ignores whatever already landed.
		for (const item of pending) {
			await db.updateSyncStatus(item.localId, 'QUEUED');
		}
		return { ...emptyResult(workflowId), queuedCount: pending.length };
	}

	if (status.status === 'FAILED') {
		// The workflow itself died (a refused batch, an exhausted run timeout). No item
		// is known to have landed, so all of them stay queued.
		for (const item of pending) {
			await db.updateSyncStatus(item.localId, 'QUEUED');
		}
		return { ...emptyResult(workflowId), queuedCount: pending.length };
	}

	const applied = new Set(status.result?.applied ?? []);
	const rejected = new Map((status.result?.rejected ?? []).map((r) => [r.client_id, r]));

	let syncedCount = 0;
	let failedCount = 0;
	let queuedCount = 0;
	const rejections: ReplayRejection[] = [];

	for (const item of pending) {
		if (applied.has(item.localId)) {
			await db.markSynced(item.localId);
			syncedCount++;
			continue;
		}

		const rejection = rejected.get(item.localId);
		if (rejection && rejection.retryable === false) {
			if (typeof db.updateQueuedTransaction === 'function') {
				await db.updateQueuedTransaction(item.localId, {
					syncStatus: 'FAILED',
					lastError: rejection.message ?? rejection.reason ?? 'rejected'
				});
			} else {
				await db.updateSyncStatus(item.localId, 'FAILED');
			}
			failedCount++;
			rejections.push({
				localId: item.localId,
				reason: rejection.reason ?? 'rejected',
				message: rejection.message,
				retryable: false
			});
			continue;
		}

		// Retryable, or simply absent from the verdict: keep it and send it again.
		await db.updateSyncStatus(item.localId, 'QUEUED');
		queuedCount++;
		if (rejection) {
			rejections.push({
				localId: item.localId,
				reason: rejection.reason ?? 'replay_attempts_exhausted',
				message: rejection.message,
				retryable: true
			});
		}
	}

	return { syncedCount, failedCount, queuedCount, workflowId, rejections };
}

async function pollWorkflowStatus(
	baseUrl: string,
	headers: HeadersInit,
	workflowId: string,
	options: SyncFlushOptions
): Promise<WorkflowStatus | null> {
	const intervalMs = options.pollIntervalMs ?? DEFAULT_POLL_INTERVAL_MS;
	const deadline = Date.now() + (options.pollTimeoutMs ?? DEFAULT_POLL_TIMEOUT_MS);

	while (Date.now() < deadline) {
		try {
			const res = await fetch(
				`${baseUrl}/api/v1/transactions/workflow-status/${encodeURIComponent(workflowId)}`,
				{ headers }
			);
			if (res.ok) {
				const status = (await res.json()) as WorkflowStatus;
				if (status.status === 'COMPLETED' || status.status === 'FAILED') {
					return status;
				}
			}
		} catch {
			// A dropped poll is not a lost batch. Keep asking until the deadline.
		}
		await sleep(intervalMs);
	}

	return null;
}

export async function requestBackgroundSync(): Promise<void> {
	if (typeof navigator === 'undefined' || !('serviceWorker' in navigator)) {
		return;
	}
	try {
		const registration = await navigator.serviceWorker.ready;
		const syncManager = (registration as unknown as { sync?: { register: (tag: string) => Promise<void> } }).sync;
		if (syncManager) {
			await syncManager.register(SYNC_TAG);
		}
	} catch {
		// Background Sync API is optional; online event listener handles fallback.
	}
}

export function registerOnlineSyncListener(onSynced?: (result: SyncFlushResult) => void): () => void {
	if (typeof window === 'undefined') {
		return () => {};
	}

	const handleOnline = async () => {
		try {
			const res = await flushOfflineQueue();
			onSynced?.(res);
		} catch {
			// Leave items queued/failed for retry
		}
	};

	window.addEventListener('online', handleOnline);

	const handleSWMessage = (event: MessageEvent) => {
		if (event.data?.type === 'SYNC_TRANSACTIONS_REQUESTED') {
			void handleOnline();
		}
	};
	if ('serviceWorker' in navigator) {
		navigator.serviceWorker.addEventListener('message', handleSWMessage);
	}

	return () => {
		window.removeEventListener('online', handleOnline);
		if ('serviceWorker' in navigator) {
			navigator.serviceWorker.removeEventListener('message', handleSWMessage);
		}
	};
}
