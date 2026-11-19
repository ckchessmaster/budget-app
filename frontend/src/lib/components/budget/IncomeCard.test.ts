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

	it('renders planned total, received amount, and adds a new income inline', async () => {
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

		const { getByRole, getByLabelText, getByText } = render(IncomeCard, {
			props: {
				yearMonth: '2026-11-01',
				incomes: sampleIncomes,
				receivedAmount: 2000,
				onChanged
			}
		});

		expect(getByText('Beeline Paycheck')).toBeTruthy();
		expect(getByText('$2,000.00')).toBeTruthy();

		await fireEvent.click(getByRole('button', { name: '+ Add Income' }));
		await fireEvent.input(getByLabelText('Income source name'), { target: { value: 'Freelance' } });
		await fireEvent.input(getByLabelText('Income amount'), { target: { value: '500' } });
		await fireEvent.click(getByRole('button', { name: 'Add' }));

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

	it('removes an income source when clicking remove', async () => {
		const fetchMock = vi.fn().mockResolvedValue({ ok: true });
		vi.stubGlobal('fetch', fetchMock);
		const onChanged = vi.fn();

		const { getByRole } = render(IncomeCard, {
			props: {
				yearMonth: '2026-11-01',
				incomes: sampleIncomes,
				receivedAmount: 0,
				onChanged
			}
		});

		await fireEvent.click(getByRole('button', { name: 'Remove Beeline Paycheck' }));
		await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
		expect(fetchMock.mock.calls[0][0]).toContain('/api/v1/budget/incomes/inc-1');
		expect(fetchMock.mock.calls[0][1].method).toBe('DELETE');
		expect(onChanged).toHaveBeenCalledTimes(1);
	});
});
