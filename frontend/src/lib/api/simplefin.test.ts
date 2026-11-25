import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { claimSimpleFINToken, discoverSimpleFINAccounts, fetchWorkflowStatus } from './simplefin';

const { getValidTokenMock, getAppConfigMock } = vi.hoisted(() => ({
	getValidTokenMock: vi.fn().mockResolvedValue('test-jwt-token'),
	getAppConfigMock: vi.fn().mockReturnValue({ apiBaseUrl: 'http://localhost:8080' })
}));

vi.mock('$lib/stores/auth.svelte', () => ({ auth: { getValidToken: getValidTokenMock } }));
vi.mock('$lib/config', () => ({ getAppConfig: getAppConfigMock }));

function jsonResponse(status: number, body: unknown): Response {
	return new Response(JSON.stringify(body), {
		status,
		headers: { 'Content-Type': 'application/json' }
	});
}

describe('simplefin claim is asynchronous', () => {
	const originalFetch = globalThis.fetch;

	beforeEach(() => {
		vi.useFakeTimers();
		getValidTokenMock.mockClear();
	});

	afterEach(() => {
		vi.useRealTimers();
		globalThis.fetch = originalFetch;
	});

	it('accepts the claim, polls the shared workflow endpoint, and resolves with accounts', async () => {
		const calls: { method: string; url: string }[] = [];
		globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
			const url = String(input);
			calls.push({ method: init?.method ?? 'GET', url });

			if (url.endsWith('/api/v1/bank/simplefin/claim')) {
				return jsonResponse(202, { workflow_id: 'simplefin-claim-ws-1', status: 'accepted' });
			}
			if (url.includes('/api/v1/bank/workflow-status/simplefin-claim-ws-1')) {
				const poll = calls.filter((c) => c.url.includes('workflow-status')).length;
				if (poll === 1) return jsonResponse(200, { workflow_id: 'ws', status: 'RUNNING' });
				return jsonResponse(200, {
					workflow_id: 'ws',
					status: 'COMPLETED',
					result: {
						connection_id: 'conn-1',
						accounts: [{ id: 'acct-1', name: 'Everyday Checking', currency: 'USD', balance: '102.50' }],
						accounts_discovered: 1
					}
				});
			}
			return jsonResponse(404, { error: 'unexpected' });
		}) as unknown as typeof fetch;

		const progress: string[] = [];
		const promise = claimSimpleFINToken('token-abc', (s) => progress.push(s.status));

		await vi.advanceTimersByTimeAsync(1500);
		const accounts = await promise;

		expect(calls[0]).toEqual({ method: 'POST', url: 'http://localhost:8080/api/v1/bank/simplefin/claim' });
		expect(calls[1]?.url).toContain('/api/v1/bank/workflow-status/simplefin-claim-ws-1');
		expect(accounts).toEqual([{ id: 'acct-1', name: 'Everyday Checking', currency: 'USD', balance: '102.50' }]);
		expect(progress).toEqual(['RUNNING', 'COMPLETED']);
	});

	it('surfaces a retrying bank instead of failing the claim', async () => {
		globalThis.fetch = vi.fn(async (input: RequestInfo | URL) => {
			const url = String(input);
			if (url.endsWith('/api/v1/bank/simplefin/claim')) {
				return jsonResponse(202, { workflow_id: 'wf-2', status: 'accepted' });
			}
			const poll = (globalThis.fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls.length;
			if (poll <= 2) {
				return jsonResponse(200, { workflow_id: 'wf-2', status: 'RETRYING', attempt: 2, last_error: 'rate limited' });
			}
			return jsonResponse(200, {
				workflow_id: 'wf-2',
				status: 'COMPLETED',
				result: { connection_id: 'c', accounts: [{ id: 'a', name: 'N', currency: 'USD', balance: '1' }], accounts_discovered: 1 }
			});
		}) as unknown as typeof fetch;

		const seen: string[] = [];
		const promise = claimSimpleFINToken('token', (s) => seen.push(s.status));
		await vi.advanceTimersByTimeAsync(5000);

		await expect(promise).resolves.toHaveLength(1);
		expect(seen).toContain('RETRYING');
	});

	it('rejects with the workflow error when the claim fails', async () => {
		globalThis.fetch = vi.fn(async (input: RequestInfo | URL) => {
			const url = String(input);
			if (url.endsWith('/api/v1/bank/simplefin/claim')) {
				return jsonResponse(202, { workflow_id: 'wf-3', status: 'accepted' });
			}
			return jsonResponse(200, { workflow_id: 'wf-3', status: 'FAILED', last_error: 'simplefin rejected the credentials' });
		}) as unknown as typeof fetch;

		// Attach the rejection expectation before advancing timers, otherwise the
		// rejection lands with no handler attached and vitest reports it as unhandled.
		const assertion = expect(claimSimpleFINToken('spent-token')).rejects.toThrow(
			'simplefin rejected the credentials'
		);
		await vi.advanceTimersByTimeAsync(10);
		await assertion;
	});

	it('reports a rejected handoff before any polling happens', async () => {
		const urls: string[] = [];
		globalThis.fetch = vi.fn(async (input: RequestInfo | URL) => {
			const url = String(input);
			urls.push(url);
			return jsonResponse(400, { error: 'setup_token is required' });
		}) as unknown as typeof fetch;

		await expect(claimSimpleFINToken('')).rejects.toThrow('setup_token is required');
		expect(urls).toHaveLength(1);
	});

	it('discovers accounts from an existing SimpleFIN connection via workflow polling', async () => {
		const calls: { method: string; url: string }[] = [];
		globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
			const url = String(input);
			calls.push({ method: init?.method ?? 'GET', url });

			if (url.endsWith('/api/v1/bank/simplefin/discover')) {
				return jsonResponse(202, { workflow_id: 'simplefin-discover-ws-1', status: 'accepted' });
			}
			if (url.includes('/api/v1/bank/workflow-status/simplefin-discover-ws-1')) {
				return jsonResponse(200, {
					workflow_id: 'simplefin-discover-ws-1',
					status: 'COMPLETED',
					result: {
						accounts: [
							{ id: 'acct-1', name: 'Checking', currency: 'USD', balance: '100.00' },
							{ id: 'acct-5', name: 'Amex Platinum', currency: 'USD', balance: '-512.34' }
						],
						accounts_discovered: 2
					}
				});
			}
			return jsonResponse(404, { error: 'unexpected' });
		}) as unknown as typeof fetch;

		const accounts = await discoverSimpleFINAccounts();
		expect(calls[0]).toEqual({
			method: 'POST',
			url: 'http://localhost:8080/api/v1/bank/simplefin/discover'
		});
		expect(calls[1]?.url).toContain('/api/v1/bank/workflow-status/simplefin-discover-ws-1');
		expect(accounts).toHaveLength(2);
		expect(accounts[1]?.id).toBe('acct-5');
	});

	it('reads workflow status from the generic endpoint', async () => {
		globalThis.fetch = vi.fn(async () =>
			jsonResponse(200, { workflow_id: 'wf-9', status: 'RUNNING' })
		) as unknown as typeof fetch;

		const status = await fetchWorkflowStatus<{ ok: boolean }>('wf-9');
		expect(status.status).toBe('RUNNING');
		expect(String((globalThis.fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls[0][0])).toContain(
			'/api/v1/bank/workflow-status/wf-9'
		);
	});

	it('omits Content-Type on bodyless discover requests and retries transient 520 errors', async () => {
		const seenHeaders: Array<Record<string, string>> = [];
		let discoverAttempts = 0;

		globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
			const url = String(input);
			if (url.endsWith('/api/v1/bank/simplefin/discover')) {
				discoverAttempts += 1;
				seenHeaders.push((init?.headers as Record<string, string>) ?? {});
				if (discoverAttempts === 1) {
					return new Response('', { status: 520 });
				}
				return jsonResponse(202, { workflow_id: 'simplefin-discover-ws-retry', status: 'accepted' });
			}
			if (url.includes('/api/v1/bank/workflow-status/simplefin-discover-ws-retry')) {
				return jsonResponse(200, {
					workflow_id: 'simplefin-discover-ws-retry',
					status: 'COMPLETED',
					result: {
						accounts: [{ id: 'acct-5', name: 'New Savings', currency: 'USD', balance: '250.00' }],
						accounts_discovered: 1
					}
				});
			}
			return jsonResponse(404, { error: 'unexpected' });
		}) as unknown as typeof fetch;

		const promise = discoverSimpleFINAccounts();
		await vi.advanceTimersByTimeAsync(500);
		const accounts = await promise;

		expect(discoverAttempts).toBe(2);
		expect(seenHeaders[0]?.['Content-Type']).toBeUndefined();
		expect(seenHeaders[0]?.['Authorization']).toBe('Bearer test-jwt-token');
		expect(accounts).toEqual([{ id: 'acct-5', name: 'New Savings', currency: 'USD', balance: '250.00' }]);
	});

	it('does not retry non-transient 404 errors on discover', async () => {
		let discoverAttempts = 0;
		globalThis.fetch = vi.fn(async (input: RequestInfo | URL) => {
			const url = String(input);
			if (url.endsWith('/api/v1/bank/simplefin/discover')) {
				discoverAttempts += 1;
				return jsonResponse(404, { error: 'simplefin_not_connected' });
			}
			return jsonResponse(404, { error: 'unexpected' });
		}) as unknown as typeof fetch;

		await expect(discoverSimpleFINAccounts()).rejects.toThrow('simplefin_not_connected');
		expect(discoverAttempts).toBe(1);
	});

	it('tolerates transient 520 responses while polling workflow status', async () => {
		let pollAttempts = 0;
		globalThis.fetch = vi.fn(async (input: RequestInfo | URL) => {
			const url = String(input);
			if (url.endsWith('/api/v1/bank/simplefin/discover')) {
				return jsonResponse(202, { workflow_id: 'simplefin-discover-ws-poll', status: 'accepted' });
			}
			if (url.includes('/api/v1/bank/workflow-status/simplefin-discover-ws-poll')) {
				pollAttempts += 1;
				if (pollAttempts === 1) {
					return new Response('', { status: 520 });
				}
				return jsonResponse(200, {
					workflow_id: 'simplefin-discover-ws-poll',
					status: 'COMPLETED',
					result: {
						accounts: [{ id: 'acct-6', name: 'Brokerage', currency: 'USD', balance: '1000.00' }],
						accounts_discovered: 1
					}
				});
			}
			return jsonResponse(404, { error: 'unexpected' });
		}) as unknown as typeof fetch;

		const promise = discoverSimpleFINAccounts();
		await vi.advanceTimersByTimeAsync(1500);
		const accounts = await promise;

		expect(pollAttempts).toBe(2);
		expect(accounts).toEqual([{ id: 'acct-6', name: 'Brokerage', currency: 'USD', balance: '1000.00' }]);
	});
});
