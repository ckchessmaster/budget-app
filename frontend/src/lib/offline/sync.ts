import { getAppConfig } from '$lib/config';
import { auth } from '$lib/stores/auth.svelte';
import { localDB } from './db';

export async function flushOfflineQueue(): Promise<number> {
	const queue = await localDB.getPendingQueue();
	if (queue.length === 0) return 0;

	const { apiBaseUrl } = getAppConfig();
	const headers: Record<string, string> = {
		'Content-Type': 'application/json'
	};
	if (auth.token) {
		headers['Authorization'] = `Bearer ${auth.token}`;
	}

	const res = await fetch(`${apiBaseUrl}/api/v1/transactions/sync`, {
		method: 'POST',
		headers,
		body: JSON.stringify({
			transactions: queue.map((item) => ({
				local_id: item.localId,
				account_id: item.accountId,
				amount: item.amount,
				merchant_raw: item.merchantRaw,
				category_id: item.categoryId,
				transaction_date: item.transactionDate
			}))
		})
	});

	if (!res.ok && res.status !== 207) {
		throw new Error(`Sync failed (${res.status})`);
	}

	const data = await res.json();
	let syncedCount = 0;
	for (const result of data.results ?? []) {
		if (result.status === 'SYNCED') {
			await localDB.markSynced(result.local_id);
			syncedCount++;
		}
	}
	return syncedCount;
}

export function registerOnlineSyncListener(onSynced?: (count: number) => void): () => void {
	if (typeof window === 'undefined') return () => {};

	const handler = async () => {
		try {
			const count = await flushOfflineQueue();
			if (count > 0) {
				onSynced?.(count);
			}
		} catch (err) {
			console.warn('Background offline queue sync failed:', err);
		}
	};

	window.addEventListener('online', handler);
	return () => window.removeEventListener('online', handler);
}
