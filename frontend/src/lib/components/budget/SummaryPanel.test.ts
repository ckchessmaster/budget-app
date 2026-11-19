import { fireEvent, render } from '@testing-library/svelte';
import { describe, expect, it, vi } from 'vitest';
import SummaryPanel from './SummaryPanel.svelte';
import type { CategoryBudget, MonthSummary } from '$lib/types/budget';

const categories: CategoryBudget[] = [
	{
		id: 'c1',
		name: 'Rent',
		groupName: 'Housing',
		rolloverMode: 'SWEEP',
		sweepTargetCategoryId: null,
		targetAmount: null,
		targetDate: null,
		targetCadence: 'NONE',
		sortOrder: 1,
		originalPlanned: 2000,
		effectiveTarget: 2000,
		spentCleared: 2000,
		spentPending: 0,
		remaining: 0
	},
	{
		id: 'c2',
		name: 'Groceries',
		groupName: 'Food',
		rolloverMode: 'SWEEP',
		sweepTargetCategoryId: null,
		targetAmount: null,
		targetDate: null,
		targetCadence: 'NONE',
		sortOrder: 2,
		originalPlanned: 1000,
		effectiveTarget: 1000,
		spentCleared: 400,
		spentPending: 100,
		remaining: 500
	}
];

const summary: MonthSummary = {
	yearMonth: '2026-11-01',
	totalIncomeExpected: 3000,
	totalAllocated: 3000,
	readyToAssign: 0,
	totalSpentCleared: 2400,
	totalSpentPending: 100,
	isUnbalanced: false
};

describe('SummaryPanel', () => {
	it('renders group breakdown percentages, switches metric tabs, and emits onSelectGroup', async () => {
		const onSelectGroup = vi.fn();
		const { getByRole, getAllByTestId, getByTestId, getByText } = render(SummaryPanel, {
			props: {
				categories,
				summary,
				selectedGroup: null,
				onSelectGroup
			}
		});

		expect(getByText('TOTAL PLANNED')).toBeTruthy();
		expect(getByTestId('donut-center-value').textContent?.trim()).toBe('$3,000.00');

		const rows = getAllByTestId('summary-group-row');
		expect(rows).toHaveLength(2);
		expect(rows[0].textContent).toContain('Housing');
		expect(rows[0].textContent).toContain('(67%)');
		expect(rows[1].textContent).toContain('Food');
		expect(rows[1].textContent).toContain('(33%)');

		// Switch to SPENT tab
		await fireEvent.click(getByRole('tab', { name: 'SPENT' }));
		expect(getByText('TOTAL SPENT')).toBeTruthy();
		expect(getByTestId('donut-center-value').textContent?.trim()).toBe('$2,500.00');
		expect(rows[0].textContent).toContain('(80%)');

		// Switch to REMAINING tab
		await fireEvent.click(getByRole('tab', { name: 'REMAINING' }));
		expect(getByText('TOTAL REMAINING')).toBeTruthy();
		expect(getByTestId('donut-center-value').textContent?.trim()).toBe('$500.00');
		expect(rows[1].textContent).toContain('$500.00');
		expect(rows[1].textContent).toContain('(100%)');

		// Click group row
		await fireEvent.click(rows[1]);
		expect(onSelectGroup).toHaveBeenCalledWith('Food');
	});
});
