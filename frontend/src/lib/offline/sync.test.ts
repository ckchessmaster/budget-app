import { describe, expect, it, vi } from 'vitest';
import type { BudgetLocalDB, OfflineTransaction } from './db';
import { flushOfflineQueue } from './sync';

describe('flushOfflineQueue', () => {
	it('posts queued transactions to /api/v1/transactions/sync and marks synced items', async () => {
		const queued: OfflineTransaction[] = [
			{
				localId: 'loc-1',
				accountId: 'acct-1',
				amount: -15.5,
				merchantRaw: 'Cafe',
				categoryId: 'cat-1',
				transactionDate: '2026-11-18',
				syncStatus: 'QUEUED',
				createdAt: 100
			},
			{
				localId: 'loc-2',
				accountId: 'acct-1',
				amount: -40.0,
				merchantRaw: 'Market',
				categoryId: 'cat-2',
				transactionDate: '2026-11-18',
				syncStatus: 'QUEUED',
				createdAt: 200
			}
		];

		const mockDB = {
			getPendingQueue: vi.fn().mockResolvedValue(queued),
			updateSyncStatus: vi.fn().mockResolvedValue(undefined),
			markSynced: vi.fn().mockResolvedValue(undefined)
		} as unknown as BudgetLocalDB;

		const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
			new Response(
				JSON.stringify({
					results: [
						{ local_id: 'loc-1', status: 'synced', transaction_id: 'tx-1' },
						{ local_id: 'loc-2', status: 'failed', error: 'invalid account' }
					]
				}),
				{ status: 207, headers: { 'Content-Type': 'application/json' } }
			)
		);

		const res = await flushOfflineQueue(mockDB, 'http://localhost:8080', 'test-token');
		expect(fetchSpy).toHaveBeenCalledWith(
			'http://localhost:8080/api/v1/transactions/sync',
			expect.objectContaining({
				method: 'POST',
				headers: expect.objectContaining({ Authorization: 'Bearer test-token' })
			})
		);
		expect(res).toEqual({ syncedCount: 1, failedCount: 1 });
		expect(mockDB.markSynced).toHaveBeenCalledWith('loc-1');
		expect(mockDB.updateSyncStatus).toHaveBeenCalledWith('loc-2', 'FAILED');

		fetchSpy.mockRestore();
	});
});
