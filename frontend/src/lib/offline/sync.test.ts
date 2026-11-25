import { afterEach, describe, expect, it, vi } from 'vitest';
import type { BudgetLocalDB, OfflineTransaction } from './db';
import { flushOfflineQueue } from './sync';

function queued(localId: string, createdAt: number): OfflineTransaction {
	return {
		localId,
		accountId: 'acct-1',
		amount: -15.5,
		merchantRaw: 'Cafe',
		categoryId: 'cat-1',
		transactionDate: '2026-11-18',
		syncStatus: 'QUEUED',
		createdAt
	};
}

function mockDB(items: OfflineTransaction[]) {
	return {
		getPendingQueue: vi.fn().mockResolvedValue(items),
		updateSyncStatus: vi.fn().mockResolvedValue(undefined),
		markSynced: vi.fn().mockResolvedValue(undefined)
	} as unknown as BudgetLocalDB;
}

function jsonResponse(body: unknown, status = 200): Response {
	return new Response(JSON.stringify(body), {
		status,
		headers: { 'Content-Type': 'application/json' }
	});
}

/** Fast-forward polling so the suite does not wait on real intervals. */
const fast = { pollIntervalMs: 1, pollTimeoutMs: 50 };

afterEach(() => {
	vi.restoreAllMocks();
});

describe('flushOfflineQueue', () => {
	it('submits the batch with client ids and drains it from the workflow verdict', async () => {
		const items = [queued('loc-1', 100), queued('loc-2', 200)];
		const db = mockDB(items);

		const fetchSpy = vi
			.spyOn(globalThis, 'fetch')
			.mockResolvedValueOnce(jsonResponse({ workflow_id: 'wf-9', status: 'accepted' }, 202))
			.mockResolvedValueOnce(
				jsonResponse({
					workflow_id: 'wf-9',
					status: 'COMPLETED',
					result: {
						applied: ['loc-1'],
						rejected: [
							{
								client_id: 'loc-2',
								reason: 'unknown_account',
								message: 'the queued account no longer exists',
								retryable: false
							}
						]
					}
				})
			);

		const res = await flushOfflineQueue(db, 'http://localhost:8080', 'test-token', fast);

		expect(fetchSpy.mock.calls[0][0]).toBe('http://localhost:8080/api/v1/transactions/sync');
		const sentBody = JSON.parse(String(fetchSpy.mock.calls[0]?.[1]?.body));
		expect(sentBody.items[0]).toMatchObject({ client_id: 'loc-1', account_id: 'acct-1' });

		expect(fetchSpy.mock.calls[1][0]).toBe(
			'http://localhost:8080/api/v1/transactions/workflow-status/wf-9'
		);

		expect(res.syncedCount).toBe(1);
		expect(res.failedCount).toBe(1);
		expect(res.queuedCount).toBe(0);
		expect(res.workflowId).toBe('wf-9');
		expect(res.rejections).toEqual([
			{
				localId: 'loc-2',
				reason: 'unknown_account',
				message: 'the queued account no longer exists',
				retryable: false
			}
		]);

		expect(db.markSynced).toHaveBeenCalledWith('loc-1');
		expect(db.updateSyncStatus).toHaveBeenCalledWith('loc-2', 'FAILED');
	});

	it('waits through RUNNING and keeps retryable items queued', async () => {
		const items = [queued('loc-1', 100), queued('loc-2', 200)];
		const db = mockDB(items);

		vi.spyOn(globalThis, 'fetch')
			.mockResolvedValueOnce(jsonResponse({ workflow_id: 'wf-10', status: 'accepted' }, 202))
			.mockResolvedValueOnce(jsonResponse({ workflow_id: 'wf-10', status: 'RUNNING' }))
			.mockResolvedValueOnce(
				jsonResponse({
					workflow_id: 'wf-10',
					status: 'COMPLETED',
					result: {
						applied: ['loc-1'],
						rejected: [{ client_id: 'loc-2', reason: 'replay_attempts_exhausted', retryable: true }]
					}
				})
			);

		const res = await flushOfflineQueue(db, 'http://localhost:8080', 'test-token', fast);

		expect(res.syncedCount).toBe(1);
		expect(res.queuedCount).toBe(1);
		expect(res.failedCount).toBe(0);
		expect(res.rejections[0]).toMatchObject({ localId: 'loc-2', retryable: true });
		expect(db.markSynced).toHaveBeenCalledWith('loc-1');
		expect(db.updateSyncStatus).toHaveBeenCalledWith('loc-2', 'QUEUED');
	});

	it('leaves the whole queue intact when the batch never resolves', async () => {
		const items = [queued('loc-1', 100), queued('loc-2', 200)];
		const db = mockDB(items);

		// The workflow is still running when the deadline passes. Nothing may be dropped:
		// resending is idempotent server-side, so QUEUED is always the safe answer.
		let accepted = false;
		vi.spyOn(globalThis, 'fetch').mockImplementation(async () => {
			const first = !accepted;
			accepted = true;
			return jsonResponse({ workflow_id: 'wf-11', status: 'RUNNING' }, first ? 202 : 200);
		});

		const res = await flushOfflineQueue(db, 'http://localhost:8080', 'test-token', {
			pollIntervalMs: 1,
			pollTimeoutMs: 20
		});

		expect(res.syncedCount).toBe(0);
		expect(res.queuedCount).toBe(2);
		expect(db.markSynced).not.toHaveBeenCalled();
		expect(db.updateSyncStatus).toHaveBeenLastCalledWith('loc-2', 'QUEUED');
	});

	it('keeps every item queued when the workflow itself failed', async () => {
		const items = [queued('loc-1', 100)];
		const db = mockDB(items);

		vi.spyOn(globalThis, 'fetch')
			.mockResolvedValueOnce(jsonResponse({ workflow_id: 'wf-12', status: 'accepted' }, 202))
			.mockResolvedValueOnce(
				jsonResponse({ workflow_id: 'wf-12', status: 'FAILED', last_error: 'batch_too_large' })
			);

		const res = await flushOfflineQueue(db, 'http://localhost:8080', 'test-token', fast);

		expect(res.queuedCount).toBe(1);
		expect(db.markSynced).not.toHaveBeenCalled();
		expect(db.updateSyncStatus).toHaveBeenLastCalledWith('loc-1', 'QUEUED');
	});

	it('marks the batch failed when the edge refuses it', async () => {
		const items = [queued('loc-1', 100)];
		const db = mockDB(items);

		vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(jsonResponse({ error: 'batch_too_large' }, 413));

		await expect(
			flushOfflineQueue(db, 'http://localhost:8080', 'test-token', fast)
		).rejects.toThrow('Offline sync failed (413)');

		expect(db.updateSyncStatus).toHaveBeenLastCalledWith('loc-1', 'FAILED');
	});

	it('does nothing when the queue is empty', async () => {
		const db = mockDB([]);
		const fetchSpy = vi.spyOn(globalThis, 'fetch');

		const res = await flushOfflineQueue(db, 'http://localhost:8080', 'test-token', fast);

		expect(fetchSpy).not.toHaveBeenCalled();
		expect(res).toMatchObject({ syncedCount: 0, failedCount: 0, queuedCount: 0 });
	});
});
