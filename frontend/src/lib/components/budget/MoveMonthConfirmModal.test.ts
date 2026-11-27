import { fireEvent, render } from '@testing-library/svelte';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import MoveMonthConfirmModal from './MoveMonthConfirmModal.svelte';
import {
	buildMoveToMonthPatch,
	isMoveMonthPromptSkippedToday,
	MOVE_MONTH_SKIP_DATE_KEY,
	skipMoveMonthPromptToday
} from '$lib/utils/transactionReview';

describe('MoveMonthConfirmModal & cross-month helpers', () => {
	beforeEach(() => {
		window.localStorage.removeItem(MOVE_MONTH_SKIP_DATE_KEY);
	});

	it('confirms with optional dont-remind-today flag and cancels on Escape', async () => {
		const onCancel = vi.fn();
		const onConfirm = vi.fn();

		const { getByLabelText, getByTestId, getByText } = render(MoveMonthConfirmModal, {
			props: {
				isOpen: true,
				merchantName: 'Trader Joes',
				fromMonth: '2026-10',
				toMonth: '2026-11',
				targetLabel: 'Groceries',
				onCancel,
				onConfirm
			}
		});

		expect(getByText(/Trader Joes/)).toBeTruthy();
		expect(getByText(/Groceries/)).toBeTruthy();

		await fireEvent.keyDown(window, { key: 'Escape' });
		expect(onCancel).toHaveBeenCalledTimes(1);
		expect(onConfirm).not.toHaveBeenCalled();

		const checkbox = getByLabelText("Don't remind me again today") as HTMLInputElement;
		await fireEvent.click(checkbox);
		await fireEvent.click(getByTestId('move-month-confirm-btn'));

		expect(onConfirm).toHaveBeenCalledWith(true);
	});

	it('tracks skip state per local calendar day and builds budget_month patches cleanly', () => {
		const day1 = new Date(2026, 10, 26, 10, 0, 0);
		const day2 = new Date(2026, 10, 27, 9, 0, 0);

		expect(isMoveMonthPromptSkippedToday(day1)).toBe(false);
		skipMoveMonthPromptToday(day1);
		expect(isMoveMonthPromptSkippedToday(day1)).toBe(true);
		expect(isMoveMonthPromptSkippedToday(day2)).toBe(false);

		expect(
			buildMoveToMonthPatch(
				{
					id: 'tx-1',
					accountId: 'a-1',
					origin: 'BANK_FEED',
					status: 'POSTED',
					amount: -25,
					merchantRaw: 'Store',
					categoryId: null,
					transactionDate: '2026-10-29',
					isCleared: true,
					isTransfer: false
				},
				'2026-11'
			)
		).toEqual({ budgetMonth: '2026-11' });

		expect(
			buildMoveToMonthPatch(
				{
					id: 'tx-2',
					accountId: 'a-1',
					origin: 'BANK_FEED',
					status: 'POSTED',
					amount: -25,
					merchantRaw: 'Store',
					categoryId: null,
					transactionDate: '2026-11-02',
					budgetMonth: '2026-10',
					isCleared: true,
					isTransfer: false
				},
				'2026-11'
			)
		).toEqual({ clearBudgetMonth: true });
	});
});
