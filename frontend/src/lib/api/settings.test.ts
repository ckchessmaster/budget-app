import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { resetDemoMode } from './settings';

const { getValidTokenMock, getAppConfigMock } = vi.hoisted(() => ({
	getValidTokenMock: vi.fn().mockResolvedValue('test-jwt-token'),
	getAppConfigMock: vi.fn().mockReturnValue({ apiBaseUrl: 'http://localhost:8080' })
}));

vi.mock('$lib/stores/auth.svelte', () => ({ auth: { getValidToken: getValidTokenMock } }));
vi.mock('$lib/config', () => ({ getAppConfig: getAppConfigMock }));

function json(status: number, body: unknown): Response {
	return new Response(JSON.stringify(body), {
		status,
		headers: { 'Content-Type': 'application/json' }
	});
}

describe('demo reset is asynchronous', () => {
	const originalFetch = globalThis.fetch;

	beforeEach(() => {
		vi.useFakeTimers();
	});

	afterEach(() => {
		vi.useRealTimers();
		globalThis.fetch = originalFetch;
	});

	it('starts the workflow, polls it, then reloads settings', async () => {
		const urls: string[] = [];
		globalThis.fetch = vi.fn(async (input: RequestInfo | URL) => {
			const url = String(input);
			urls.push(url);

			if (url.endsWith('/api/v1/demo/reset')) {
				return json(202, { workflow_id: 'demo-reset-ws-1', status: 'accepted' });
			}
			if (url.includes('/api/v1/bank/workflow-status/demo-reset-ws-1')) {
				const polls = urls.filter((u) => u.includes('workflow-status')).length;
				if (polls === 1) return json(200, { workflow_id: 'demo-reset-ws-1', status: 'RUNNING' });
				return json(200, {
					workflow_id: 'demo-reset-ws-1',
					status: 'COMPLETED',
					result: {
						workspace_id: 'sandbox-1',
						categories_seeded: 11,
						accounts_seeded: 4,
						connection_ready: true
					}
				});
			}
			if (url.endsWith('/api/v1/settings')) {
				return json(200, { workspace_id: 'sandbox-1', workspace_name: 'Demo Sandbox Workspace', is_demo: true, demo_mode_enabled: true });
			}
			return json(404, { error: 'unexpected ' + url });
		}) as unknown as typeof fetch;

		const promise = resetDemoMode();
		await vi.advanceTimersByTimeAsync(3000);
		const settings = await promise;

		expect(settings.workspace_id).toBe('sandbox-1');
		expect(settings.demo_mode_enabled).toBe(true);
		expect(urls[0]).toBe('http://localhost:8080/api/v1/demo/reset');
		expect(urls.some((u) => u.includes('/api/v1/bank/workflow-status/'))).toBe(true);
		expect(urls[urls.length - 1]).toBe('http://localhost:8080/api/v1/settings');
	});

	it('reports a refused concurrent reset without polling', async () => {
		const urls: string[] = [];
		globalThis.fetch = vi.fn(async (input: RequestInfo | URL) => {
			urls.push(String(input));
			return json(409, { error: 'demo_reset_already_running' });
		}) as unknown as typeof fetch;

		await expect(resetDemoMode()).rejects.toThrow(/already running/);
		expect(urls).toHaveLength(1);
	});

	it('surfaces a failed reset workflow', async () => {
		globalThis.fetch = vi.fn(async (input: RequestInfo | URL) => {
			const url = String(input);
			if (url.endsWith('/api/v1/demo/reset')) {
				return json(202, { workflow_id: 'demo-reset-ws-2', status: 'accepted' });
			}
			return json(200, {
				workflow_id: 'demo-reset-ws-2',
				status: 'FAILED',
				last_error: 'bank sync did not stop'
			});
		}) as unknown as typeof fetch;

		const assertion = expect(resetDemoMode()).rejects.toThrow('bank sync did not stop');
		await vi.advanceTimersByTimeAsync(10);
		await assertion;
	});
});
