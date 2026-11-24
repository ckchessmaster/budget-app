import { fireEvent, render, waitFor } from '@testing-library/svelte';
import { beforeEach, describe, expect, it, vi } from 'vitest';

const mockFetchSimulatorState = vi.fn();
const mockStageDemoTransaction = vi.fn();
const mockSettleDemoPending = vi.fn();
const mockStageDemoTransfer = vi.fn();
const mockStageDemoManualMatch = vi.fn();
const mockUpdateDemoFault = vi.fn();
const mockTriggerSyncAndPoll = vi.fn();

vi.mock('$lib/api/settings', () => ({
	fetchSimulatorState: (...args: unknown[]) => mockFetchSimulatorState(...args),
	stageDemoTransaction: (...args: unknown[]) => mockStageDemoTransaction(...args),
	settleDemoPending: (...args: unknown[]) => mockSettleDemoPending(...args),
	stageDemoTransfer: (...args: unknown[]) => mockStageDemoTransfer(...args),
	stageDemoManualMatch: (...args: unknown[]) => mockStageDemoManualMatch(...args),
	updateDemoFault: (...args: unknown[]) => mockUpdateDemoFault(...args)
}));

vi.mock('$lib/stores/demo.svelte', () => ({
	demo: {
		isDemo: true,
		dataVersion: 0,
		unsyncedCount: 0,
		syncStatus: 'idle',
		syncMessage: '',
		lastSyncResult: null,
		lastSyncError: null,
		triggerSyncAndPoll: (...args: unknown[]) => mockTriggerSyncAndPoll(...args)
	}
}));

import SimulatorWorkbench from './SimulatorWorkbench.svelte';

function makeSimState(overrides: Record<string, unknown> = {}) {
	return {
		accounts: [
			{
				id: 'a-1',
				external_account_id: 'demo-checking-001',
				name: 'Demo Checking',
				currency: 'USD',
				balance: '4250.00',
				account_type: 'checking',
				card_mode: 'PAY_IN_FULL'
			},
			{
				id: 'a-2',
				external_account_id: 'demo-cc-rewards-001',
				name: 'Demo Everyday Rewards',
				currency: 'USD',
				balance: '-645.20',
				account_type: 'credit_card',
				card_mode: 'PAY_IN_FULL'
			}
		],
		transactions: [
			{
				id: 'tx-pend-1',
				external_account_id: 'demo-cc-rewards-001',
				external_transaction_id: 'demo-seed-tx-6',
				posted_at: '2026-11-20T12:00:00Z',
				amount: '-40.00',
				description: 'BURGER BAR',
				pending: true,
				is_synced: true
			}
		],
		manual_transactions: [],
		fault: {
			mode: 'NONE',
			persistent: false,
			retry_after_seconds: 3,
			message: ''
		},
		unsynced_count: 1,
		...overrides
	};
}

describe('SimulatorWorkbench', () => {
	beforeEach(() => {
		vi.resetAllMocks();
		mockFetchSimulatorState.mockResolvedValue(makeSimState());
	});

	it('renders unsynced badge and stages a preset transaction', async () => {
		mockStageDemoTransaction.mockResolvedValueOnce({ external_transaction_id: 'demo-tx-new' });
		const { getByTestId } = render(SimulatorWorkbench);

		await waitFor(() => {
			expect(getByTestId('sim-unsynced-badge').textContent).toContain('1 staged (unsynced)');
		});

		await fireEvent.click(getByTestId('preset-coffee-btn'));
		await waitFor(() => {
			expect(mockStageDemoTransaction).toHaveBeenCalledWith({
				preset: 'coffee',
				sync_now: false
			});
		});
	});

	it('settles a pending transaction from Settle Pending tab', async () => {
		mockSettleDemoPending.mockResolvedValueOnce({ external_transaction_id: 'demo-tx-settled' });
		const { getByTestId, getByText } = render(SimulatorWorkbench);

		await waitFor(() => {
			expect(getByTestId('sim-tab-settle')).toBeTruthy();
		});

		await fireEvent.click(getByTestId('sim-tab-settle'));
		await waitFor(() => {
			expect(getByText('BURGER BAR')).toBeTruthy();
		});

		await fireEvent.click(getByTestId('settle-exact-tx-pend-1'));
		await waitFor(() => {
			expect(mockSettleDemoPending).toHaveBeenCalledWith({
				demo_transaction_id: 'tx-pend-1',
				tip_percent: undefined,
				append_suffix: undefined,
				sync_now: false
			});
		});
	});

	it('updates fault injection config from Faults tab', async () => {
		mockUpdateDemoFault.mockResolvedValueOnce({
			mode: 'RATE_LIMIT_429',
			persistent: true,
			retry_after_seconds: 3,
			message: ''
		});
		const { getByTestId, getByLabelText } = render(SimulatorWorkbench);

		await waitFor(() => {
			expect(getByTestId('sim-tab-faults')).toBeTruthy();
		});

		await fireEvent.click(getByTestId('sim-tab-faults'));
		await fireEvent.click(getByLabelText(/HTTP 429 — Rate Limited/i));
		await fireEvent.click(getByLabelText(/Persist fault across syncs/i));
		await fireEvent.click(getByTestId('save-fault-btn'));

		await waitFor(() => {
			expect(mockUpdateDemoFault).toHaveBeenCalledWith({
				mode: 'RATE_LIMIT_429',
				persistent: true,
				retry_after_seconds: 3,
				message: ''
			});
		});
	});
});
