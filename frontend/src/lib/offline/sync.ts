import { getAppConfig } from '$lib/config';
import { auth } from '$lib/stores/auth.svelte';
import { BudgetLocalDB } from './db';

export const SYNC_TAG = 'sync-transactions';

export interface SyncFlushResult {
	syncedCount: number;
	failedCount: number;
}

export async function flushOfflineQueue(
	db: BudgetLocalDB = new BudgetLocalDB(),
	apiBaseUrl?: string,
	token?: string | null
): Promise<SyncFlushResult> {
	const pending = await db.getPendingQueue();
	if (pending.length === 0) {
		return { syncedCount: 0, failedCount: 0 };
	}

	const baseUrl = apiBaseUrl ?? getAppConfig().apiBaseUrl;
	const authToken = token !== undefined ? token : auth.token;

	const headers: Record<string, string> = {
		'Content-Type': 'application/json'
	};
	if (authToken) {
		headers['Authorization'] = `Bearer ${authToken}`;
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
					local_id: item.localId,
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
		for (const item of pending) {
			await db.updateSyncStatus(item.localId, 'FAILED');
		}
		throw err;
	}

	if (res.status !== 200 && res.status !== 207) {
		for (const item of pending) {
			await db.updateSyncStatus(item.localId, 'FAILED');
		}
		throw new Error(`Offline sync failed (${res.status})`);
	}

	const payload = (await res.json()) as {
		results?: Array<{ local_id: string; status: 'synced' | 'failed' }>;
	};

	let syncedCount = 0;
	let failedCount = 0;

	if (Array.isArray(payload.results)) {
		for (const result of payload.results) {
			if (result.status === 'synced') {
				await db.markSynced(result.local_id);
				syncedCount++;
			} else {
				await db.updateSyncStatus(result.local_id, 'FAILED');
				failedCount++;
			}
		}
	} else {
		for (const item of pending) {
			await db.markSynced(item.localId);
			syncedCount++;
		}
	}

	return { syncedCount, failedCount };
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
