import { fireEvent, render, waitFor } from '@testing-library/svelte';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import IncomeCard from './IncomeCard.svelte';
import type { PlannedIncome } from '$lib/types/budget';

const sampleIncomes: PlannedIncome[] = [
	{
		id: 'inc-1',
		workspaceId: 'ws-1',
		yearMonth: '2026-11-01',
		name: 'Beeline Paycheck',
		amount: 4000,
		createdAt: '2026-11-01T00:00:00Z'
	}
];

describe('IncomeCard', () => {
	beforeEach(() => {
		vi.restoreAllMocks();
	});

	it('renders planned total, received amount, Paid/Upcoming badge, and adds a new income via modal', async () => {
		const fetchMock = vi.fn().mockResolvedValue({
			ok: true,
			json: async () => ({
				id: 'inc-2',
				workspace_id: 'ws-1',
				year_month: '2026-11-01',
				name: 'Freelance',
				amount: 500,
				created_at: '2026-11-15T00:00:00Z'
			})
		});
		vi.stubGlobal('fetch', fetchMock);
		const onChanged = vi.fn();

		const { getByRole, getByLabelText, getByText, getByTestId, queryByTestId } = render(
			IncomeCard,
			{
				props: {
					yearMonth: '2026-11-01',
					incomes: sampleIncomes,
					receivedAmount: 2000,
					onChanged
				}
			}
		);

		expect(getByText('Beeline Paycheck')).toBeTruthy();
		expect(getByTestId('income-received-total').textContent?.trim()).toBe('$2,000.00');
		expect(getByTestId('income-status-badge').textContent?.trim()).toBe('Upcoming');
		expect(queryByTestId('income-progress-bar')).toBeNull();
		expect(queryByTestId('income-modal')).toBeNull();

		await fireEvent.click(getByRole('button', { name: '+ Add Income' }));
		expect(getByTestId('income-modal')).toBeTruthy();

		await fireEvent.input(getByLabelText('Income Source Name'), { target: { value: 'Freelance' } });
		await fireEvent.input(getByLabelText('Planned Monthly Amount ($)'), { target: { value: '500' } });
		await fireEvent.click(getByRole('button', { name: 'Add Income' }));

		await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
		const [url, init] = fetchMock.mock.calls[0];
		expect(url).toContain('/api/v1/budget/incomes');
		expect(init.method).toBe('POST');
		expect(JSON.parse(init.body)).toEqual({
			year_month: '2026-11-01',
			name: 'Freelance',
			amount: 500
		});
		expect(onChanged).toHaveBeenCalledTimes(1);
	});

	it('marks income Paid when full amount is received and toggles inline activity drawer on row click', async () => {
		const { getByTestId, queryByTestId, getByText } = render(IncomeCard, {
			props: {
				yearMonth: '2026-11-01',
				incomes: sampleIncomes,
				receivedAmount: 4000,
				inflowTransactions: [
					{
						id: 'tx-pay-1',
						date: '2026-11-15',
						merchant: 'Beeline Paycheck Deposit',
						accountName: 'Checking',
						amount: 4000,
						status: 'POSTED'
					}
				],
				onChanged: vi.fn()
			}
		});

		expect(getByTestId('income-status-badge').textContent?.trim()).toBe('Paid');
		expect(queryByTestId('inline-activity-drawer')).toBeNull();

		await fireEvent.click(getByTestId('income-row'));
		expect(getByTestId('inline-activity-drawer')).toBeTruthy();
		expect(getByText(/Nov 15 · Beeline Paycheck Deposit/)).toBeTruthy();
		expect(getByText('(Checking)')).toBeTruthy();
		expect(getByText('+$4,000.00')).toBeTruthy();

		await fireEvent.click(getByTestId('income-row'));
		expect(queryByTestId('inline-activity-drawer')).toBeNull();
	});

	it('hides inline Remove button and opens Edit modal to update or delete without expanding drawer', async () => {
		const fetchMock = vi.fn().mockResolvedValue({
			ok: true,
			json: async () => ({
				id: 'inc-1',
				workspace_id: 'ws-1',
				year_month: '2026-11-01',
				name: 'Beeline Updated',
				amount: 4200,
				created_at: '2026-11-01T00:00:00Z'
			})
		});
		vi.stubGlobal('fetch', fetchMock);
		const onChanged = vi.fn();

		const { getByRole, getByLabelText, getByTestId, queryByRole, queryByTestId } = render(
			IncomeCard,
			{
				props: {
					yearMonth: '2026-11-01',
					incomes: sampleIncomes,
					receivedAmount: 0,
					onChanged
				}
			}
		);

		expect(queryByRole('button', { name: 'Remove Beeline Paycheck' })).toBeNull();

		await fireEvent.click(getByRole('button', { name: 'Edit Beeline Paycheck' }));
		expect(queryByTestId('inline-activity-drawer')).toBeNull();
		expect(getByTestId('income-modal')).toBeTruthy();

		await fireEvent.input(getByLabelText('Income Source Name'), {
			target: { value: 'Beeline Updated' }
		});
		await fireEvent.input(getByLabelText('Planned Monthly Amount ($)'), {
			target: { value: '4200' }
		});

		// Escape cancels without saving
		await fireEvent.keyDown(window, { key: 'Escape' });
		expect(queryByTestId('income-modal')).toBeNull();
		expect(fetchMock).not.toHaveBeenCalled();

		// Reopen, save
		await fireEvent.click(getByRole('button', { name: 'Edit Beeline Paycheck' }));
		await fireEvent.input(getByLabelText('Income Source Name'), {
			target: { value: 'Beeline Updated' }
		});
		await fireEvent.input(getByLabelText('Planned Monthly Amount ($)'), {
			target: { value: '4200' }
		});
		await fireEvent.click(getByRole('button', { name: 'Save Income' }));

		await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
		expect(fetchMock.mock.calls[0][0]).toContain('/api/v1/budget/incomes/inc-1');
		expect(fetchMock.mock.calls[0][1].method).toBe('PATCH');
		expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({
			name: 'Beeline Updated',
			amount: 4200
		});
		expect(onChanged).toHaveBeenCalledTimes(1);

		// Reopen and delete via modal Delete button
		await fireEvent.click(getByRole('button', { name: 'Edit Beeline Paycheck' }));
		await fireEvent.click(getByRole('button', { name: 'Delete' }));
		await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
		expect(fetchMock.mock.calls[1][0]).toContain('/api/v1/budget/incomes/inc-1');
		expect(fetchMock.mock.calls[1][1].method).toBe('DELETE');
		expect(onChanged).toHaveBeenCalledTimes(2);
	});

	it('creates a $0.00 planned income source such as Unplanned Income via modal', async () => {
		const fetchMock = vi.fn().mockResolvedValue({
			ok: true,
			json: async () => ({
				id: 'inc-zero',
				workspace_id: 'ws-1',
				year_month: '2026-11-01',
				name: 'Unplanned Income',
				amount: 0,
				created_at: '2026-11-25T00:00:00Z'
			})
		});
		vi.stubGlobal('fetch', fetchMock);
		const onChanged = vi.fn();

		const { getByRole, getByLabelText } = render(IncomeCard, {
			props: {
				yearMonth: '2026-11-01',
				incomes: sampleIncomes,
				receivedAmount: 0,
				onChanged
			}
		});

		await fireEvent.click(getByRole('button', { name: '+ Add Income' }));
		await fireEvent.input(getByLabelText('Income Source Name'), {
			target: { value: 'Unplanned Income' }
		});
		await fireEvent.input(getByLabelText('Planned Monthly Amount ($)'), { target: { value: '0' } });
		await fireEvent.click(getByRole('button', { name: 'Add Income' }));

		await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
		expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({
			year_month: '2026-11-01',
			name: 'Unplanned Income',
			amount: 0
		});
		expect(onChanged).toHaveBeenCalledTimes(1);
	});

	it('accepts transaction drops onto an income row and ignores category drag events', async () => {
		const onDropTransaction = vi.fn().mockResolvedValue(undefined);
		const { getByTestId } = render(IncomeCard, {
			props: {
				yearMonth: '2026-11-01',
				incomes: sampleIncomes,
				receivedAmount: 0,
				onDropTransaction,
				onChanged: vi.fn()
			}
		});

		const container = getByTestId('income-row-container');
		await fireEvent.drop(container, {
			dataTransfer: {
				getData: (type: string) =>
					type === 'application/x-budget-category-id' ? 'cat-1' : ''
			}
		});
		expect(onDropTransaction).not.toHaveBeenCalled();

		await fireEvent.drop(container, {
			dataTransfer: {
				getData: (type: string) => (type === 'application/x-budget-tx-id' ? 'tx-inflow-1' : '')
			}
		});
		expect(onDropTransaction).toHaveBeenCalledWith('tx-inflow-1', 'inc-1');
	});

	it('attributes explicitly assigned transactions only to their assigned income row', async () => {
		const incomes: PlannedIncome[] = [
			...sampleIncomes,
			{
				id: 'inc-unplanned',
				workspaceId: 'ws-1',
				yearMonth: '2026-11-01',
				name: 'Unplanned Income',
				amount: 0,
				createdAt: '2026-11-02T00:00:00Z'
			}
		];

		const { getAllByTestId, getByText, queryByText } = render(IncomeCard, {
			props: {
				yearMonth: '2026-11-01',
				incomes,
				receivedAmount: 4086,
				inflowTransactions: [
					{
						id: 'tx-pay',
						plannedIncomeId: 'inc-1',
						date: '2026-11-15',
						merchant: 'Beeline Payroll',
						accountName: 'Checking',
						amount: 4000,
						status: 'POSTED'
					},
					{
						id: 'tx-venmo',
						plannedIncomeId: 'inc-unplanned',
						date: '2026-11-20',
						merchant: 'Transfer From Venmo',
						accountName: 'Checking',
						amount: 86,
						status: 'POSTED'
					}
				],
				onChanged: vi.fn()
			}
		});

		const rows = getAllByTestId('income-row');
		expect(rows).toHaveLength(2);
		expect(rows[0].textContent).toContain('$4,000.00');
		expect(rows[1].textContent).toContain('$86.00');

		await fireEvent.click(rows[1]);
		expect(getByText(/Transfer From Venmo/)).toBeTruthy();
		expect(queryByText(/Beeline Payroll/)).toBeNull();
	});

	it('shows a soft highlight on transaction dragover, ignores group drags, and resists child dragleave flicker', async () => {
		const onDropTransaction = vi.fn().mockResolvedValue(undefined);
		const { getByTestId } = render(IncomeCard, {
			props: {
				yearMonth: '2026-11-01',
				incomes: sampleIncomes,
				receivedAmount: 0,
				onDropTransaction,
				onChanged: vi.fn()
			}
		});

		const container = getByTestId('income-row-container');
		const row = getByTestId('income-row');

		await fireEvent.dragOver(container, {
			dataTransfer: { types: ['application/x-budget-group-name'] }
		});
		expect(container.className).not.toContain('bg-accent-tint');

		await fireEvent.dragOver(container, {
			dataTransfer: { types: ['application/x-budget-tx-id'] }
		});
		expect(container.className).toContain('bg-accent-tint');
		expect(container.className).not.toContain('ring-2');

		const childDragLeave = new Event('dragleave', { bubbles: true });
		Object.defineProperty(childDragLeave, 'relatedTarget', { value: row });
		await fireEvent(container, childDragLeave);
		expect(container.className).toContain('bg-accent-tint');

		await fireEvent.drop(container, {
			dataTransfer: {
				getData: (type: string) =>
					type === 'application/x-budget-group-name' ? 'Essentials' : ''
			}
		});
		expect(onDropTransaction).not.toHaveBeenCalled();
	});
});
