import { describe, expect, it } from 'vitest';
import { BudgetLocalDB, type OfflineTransaction } from './db';

/**
 * Lightweight in-memory IDBFactory mock for testing BudgetLocalDB in Vitest/jsdom.
 */
function createMockIDBFactory(): IDBFactory {
	const databases = new Map<string, Map<string, Map<string, unknown>>>();

	function makeRequest<T>(executor: () => T): IDBRequest<T> {
		const req: any = {
			result: undefined,
			error: null,
			onsuccess: null,
			onerror: null
		};
		queueMicrotask(() => {
			try {
				req.result = executor();
				req.onsuccess?.({ target: req });
			} catch (err) {
				req.error = err;
				req.onerror?.({ target: req });
			}
		});
		return req;
	}

	return {
		open(name: string) {
			if (!databases.has(name)) {
				databases.set(name, new Map());
			}
			const stores = databases.get(name)!;
			const req: any = {
				result: null,
				error: null,
				onupgradeneeded: null,
				onsuccess: null,
				onerror: null
			};

			const db: any = {
				objectStoreNames: {
					contains: (storeName: string) => stores.has(storeName)
				},
				createObjectStore: (storeName: string) => {
					if (!stores.has(storeName)) {
						stores.set(storeName, new Map());
					}
					return {};
				},
				transaction: (storeName: string) => {
					const storeMap = stores.get(storeName) ?? new Map<string, unknown>();
					stores.set(storeName, storeMap);

					const tx: any = {
						oncomplete: null,
						onerror: null,
						onabort: null,
						objectStore: () => ({
							put: (val: any) => {
								const key = val.localId ?? val.id ?? val.yearMonth;
								return makeRequest(() => {
									storeMap.set(key, structuredClone(val));
									return key;
								});
							},
							get: (key: string) => makeRequest(() => structuredClone(storeMap.get(key))),
							getAll: () => makeRequest(() => Array.from(storeMap.values()).map((v) => structuredClone(v))),
							delete: (key: string) =>
								makeRequest(() => {
									storeMap.delete(key);
									return undefined;
								}),
							clear: () =>
								makeRequest(() => {
									storeMap.clear();
									return undefined;
								})
						})
					};

					setTimeout(() => {
						tx.oncomplete?.({ target: tx });
					}, 0);
					return tx;
				}
			};

			queueMicrotask(() => {
				req.result = db;
				req.onupgradeneeded?.({ target: req });
				req.onsuccess?.({ target: req });
			});

			return req;
		}
	} as unknown as IDBFactory;
}

describe('BudgetLocalDB', () => {
	it('queues 3 offline transactions, retrieves pending queue, and marks one synced', async () => {
		const db = new BudgetLocalDB(createMockIDBFactory());
		const baseTime = 1700000000000;

		const txs: OfflineTransaction[] = [
			{
				localId: 'local-1',
				accountId: 'acct-1',
				amount: -12.5,
				merchantRaw: 'Coffee Shop',
				categoryId: 'cat-1',
				transactionDate: '2026-11-18',
				syncStatus: 'QUEUED',
				createdAt: baseTime + 1
			},
			{
				localId: 'local-2',
				accountId: 'acct-1',
				amount: -45.0,
				merchantRaw: 'Grocery Store',
				categoryId: 'cat-2',
				transactionDate: '2026-11-18',
				syncStatus: 'QUEUED',
				createdAt: baseTime + 2
			},
			{
				localId: 'local-3',
				accountId: 'acct-1',
				amount: -8.99,
				merchantRaw: 'Bakery',
				categoryId: 'cat-1',
				transactionDate: '2026-11-18',
				syncStatus: 'QUEUED',
				createdAt: baseTime + 3
			}
		];

		for (const tx of txs) {
			await db.queueTransaction(tx);
		}

		const pendingBefore = await db.getPendingQueue();
		expect(pendingBefore).toHaveLength(3);
		expect(pendingBefore.map((t) => t.localId)).toEqual(['local-1', 'local-2', 'local-3']);

		await db.markSynced('local-2');

		const pendingAfter = await db.getPendingQueue();
		expect(pendingAfter).toHaveLength(2);
		expect(pendingAfter.map((t) => t.localId)).toEqual(['local-1', 'local-3']);
	});

	it('re-picks items left in SYNCING by a tab that closed mid-flight', async () => {
		// The replay is asynchronous, so SYNCING is no longer transient within a single
		// request. An item stranded in that state must be offered again: the server
		// dedupes on the client id, so a resend confirms it instead of duplicating it.
		const db = new BudgetLocalDB(createMockIDBFactory());
		const statuses = ['QUEUED', 'SYNCING', 'FAILED'] as const;

		for (const [index, syncStatus] of statuses.entries()) {
			await db.queueTransaction({
				localId: `local-${index}`,
				accountId: 'acct-1',
				amount: -5,
				merchantRaw: 'Cafe',
				categoryId: null,
				transactionDate: '2026-11-18',
				syncStatus,
				createdAt: 1000 + index
			});
		}

		const pending = await db.getPendingQueue();
		expect(pending.map((t) => t.localId)).toEqual(['local-0', 'local-1', 'local-2']);
	});

	it('caches and retrieves accounts and categories', async () => {
		const db = new BudgetLocalDB(createMockIDBFactory());
		await db.cacheAccounts([
			{ id: 'acct-1', name: 'Checking', type: 'checking', currentBalance: 1200, isOnBudget: true }
		]);
		await db.cacheCategories([
			{ id: 'cat-1', name: 'Groceries', groupName: 'Food', remaining: 350 }
		]);

		const accounts = await db.getCachedAccounts();
		const categories = await db.getCachedCategories();
		expect(accounts).toHaveLength(1);
		expect(accounts[0].name).toBe('Checking');
		expect(categories).toHaveLength(1);
		expect(categories[0].name).toBe('Groceries');
	});

	it('updates, deletes, and scopes queued transactions by workspace and caches snapshots', async () => {
		const db = new BudgetLocalDB(createMockIDBFactory());
		await db.queueTransaction({
			localId: 'loc-ws1',
			workspaceId: 'ws-1',
			accountId: 'acct-1',
			amount: -10,
			merchantRaw: 'Cafe',
			categoryId: 'cat-1',
			transactionDate: '2026-11-18',
			syncStatus: 'FAILED',
			lastError: 'unknown_account',
			createdAt: 100
		});
		await db.queueTransaction({
			localId: 'loc-ws2',
			workspaceId: 'ws-2',
			accountId: 'acct-2',
			amount: -20,
			merchantRaw: 'Demo Store',
			categoryId: null,
			transactionDate: '2026-11-18',
			syncStatus: 'QUEUED',
			createdAt: 200
		});

		expect((await db.getPendingQueue('ws-1')).map((t) => t.localId)).toEqual(['loc-ws1']);

		await db.updateQueuedTransaction('loc-ws1', {
			merchantRaw: 'Updated Cafe',
			syncStatus: 'QUEUED'
		});
		const updated = (await db.getPendingQueue('ws-1'))[0];
		expect(updated.merchantRaw).toBe('Updated Cafe');
		expect(updated.syncStatus).toBe('QUEUED');
		expect(updated.lastError).toBeUndefined();

		await db.clearQueuedForWorkspace('ws-2');
		expect(await db.getPendingQueue('ws-2')).toHaveLength(0);

		await db.deleteQueuedTransaction('loc-ws1');
		expect(await db.getPendingQueue('ws-1')).toHaveLength(0);

		await db.cacheBudgetSnapshot({
			yearMonth: '2026-11',
			summary: {
				yearMonth: '2026-11-01',
				totalIncomeExpected: 5000,
				totalAllocated: 4500,
				readyToAssign: 500,
				totalSpentCleared: 1200,
				totalSpentPending: 50,
				isUnbalanced: false
			},
			incomes: [],
			updatedAt: 12345
		});
		const snap = await db.getCachedBudgetSnapshot('2026-11');
		expect(snap?.summary.readyToAssign).toBe(500);
	});
});
