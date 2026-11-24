import { fireEvent, render } from '@testing-library/svelte';
import { describe, expect, it, vi } from 'vitest';
import BudgetHeader from './BudgetHeader.svelte';
import type { MonthSummary } from '$lib/types/budget';
import type { SolvencyReport } from '$lib/types/transaction';

const baseSummary: MonthSummary = {
	yearMonth: '2026-11-01',
	totalIncomeExpected: 5000,
	totalAllocated: 4500,
	readyToAssign: 500,
	totalSpentCleared: 1200,
	totalSpentPending: 150,
	isUnbalanced: false
};

const baseSolvency: SolvencyReport = {
	totalLiquidCash: 15000,
	totalCreditCardDebt: 2695.2,
	solvencyBuffer: 12304.8,
	isFloatingDebt: false,
	floatingDebtAmount: 0
};

describe('BudgetHeader', () => {
	it('renders consolidated header with month selector, left-to-budget pill, and solvency popover', async () => {
		const onMonthChange = vi.fn();

		const { getByTestId, getByText } = render(BudgetHeader, {
			props: {
				currentMonth: '2026-11',
				summary: baseSummary,
				solvency: baseSolvency,
				accountCount: 3,
				toReviewCount: 0,
				onMonthChange
			}
		});

		expect(getByTestId('budget-header')).toBeTruthy();
		expect(getByText('Budget App')).toBeTruthy();
		expect(getByTestId('month-label').textContent?.trim()).toBe('Nov 2026');

		const banner = getByTestId('ready-to-assign-banner');
		expect(banner.textContent).toContain('$500.00');
		expect(banner.textContent).toContain('left to budget');

		expect(getByTestId('solvency-popover-trigger').textContent).toContain('$12,304.80');
		expect(getByTestId('theme-toggle-btn')).toBeTruthy();

		await fireEvent.click(getByTestId('prev-month-btn'));
		expect(onMonthChange).toHaveBeenCalledWith('2026-10');

		await fireEvent.click(getByTestId('next-month-btn'));
		expect(onMonthChange).toHaveBeenCalledWith('2026-12');
	});

	it('displays balanced and over-budgeted states on the status pill', () => {
		const { getByTestId, rerender } = render(BudgetHeader, {
			props: {
				currentMonth: '2026-11',
				summary: { ...baseSummary, readyToAssign: 0 },
				onMonthChange: vi.fn()
			}
		});

		expect(getByTestId('ready-to-assign-banner').textContent).toContain('$0.00');
		expect(getByTestId('ready-to-assign-banner').textContent).toContain('left to budget');

		rerender({
			currentMonth: '2026-11',
			summary: { ...baseSummary, readyToAssign: -125.5 },
			onMonthChange: vi.fn()
		});

		expect(getByTestId('ready-to-assign-banner').textContent).toContain('-$125.50');
		expect(getByTestId('ready-to-assign-banner').textContent).toContain('over-budgeted');
	});

	it('renders Catch Up button only when toReviewCount > 0 and triggers onOpenCatchUp', async () => {
		const onOpenCatchUp = vi.fn();

		const { queryByTestId, getByTestId, rerender } = render(BudgetHeader, {
			props: {
				currentMonth: '2026-11',
				summary: baseSummary,
				toReviewCount: 0,
				onMonthChange: vi.fn(),
				onOpenCatchUp
			}
		});

		expect(queryByTestId('catch-up-btn')).toBeNull();

		rerender({
			currentMonth: '2026-11',
			summary: baseSummary,
			toReviewCount: 3,
			onMonthChange: vi.fn(),
			onOpenCatchUp
		});

		const catchUpBtn = getByTestId('catch-up-btn');
		expect(catchUpBtn.textContent).toContain('Catch Up (3)');
		await fireEvent.click(catchUpBtn);
		expect(onOpenCatchUp).toHaveBeenCalledTimes(1);
	});

	it('renders mobile transactions drawer toggle with badge and triggers onToggleTransactionsDrawer', async () => {
		const onToggleTransactionsDrawer = vi.fn();

		const { getByTestId } = render(BudgetHeader, {
			props: {
				currentMonth: '2026-11',
				summary: baseSummary,
				toReviewCount: 4,
				isDrawerOpen: false,
				onMonthChange: vi.fn(),
				onToggleTransactionsDrawer
			}
		});

		const toggleBtn = getByTestId('mobile-transactions-toggle');
		expect(toggleBtn.className).toContain('lg:hidden');
		expect(toggleBtn.getAttribute('aria-expanded')).toBe('false');
		expect(toggleBtn.textContent).toContain('4');

		await fireEvent.click(toggleBtn);
		expect(onToggleTransactionsDrawer).toHaveBeenCalledTimes(1);
	});
});
