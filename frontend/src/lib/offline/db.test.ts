import 'fake-indexeddb/auto';
import { describe, expect, it } from 'vitest';
import { BudgetLocalDB } from './db';

describe('BudgetLocalDB', () => {
	it('queues 3 offline transactions, retrieves pending queue, and marks one synced', async () => {
		const db = new BudgetLocalDB();

		await db.queueTransaction({
			localId: 'local-1',
			accountId: 'acct-1',
			amount: -45.25,
			merchantRaw: 'Trader Joes',
			categoryId: 'cat-1',
			transactionDate: '2026-11-16',
			syncStatus: 'QUEUED',
			createdAt: 1000
		});
		await db.queueTransaction({
			localId: 'local-2',
			accountId: 'acct-1',
			amount: -12.5,
			merchantRaw: 'Coffee Shop',
			categoryId: 'cat-2',
			transactionDate: '2026-11-16',
			syncStatus: 'QUEUED',
			createdAt: 2000
		});
		await db.queueTransaction({
			localId: 'local-3',
			accountId: 'acct-1',
			amount: -80.0,
			merchantRaw: 'Gas Station',
			categoryId: 'cat-3',
			transactionDate: '2026-11-16',
			syncStatus: 'QUEUED',
			createdAt: 3000
		});

		const queueBefore = await db.getPendingQueue();
		expect(queueBefore).toHaveLength(3);

		await db.markSynced('local-2');

		const queueAfter = await db.getPendingQueue();
		expect(queueAfter).toHaveLength(2);
		expect(queueAfter.map((t) => t.localId)).toEqual(['local-1', 'local-3']);
	});
});
