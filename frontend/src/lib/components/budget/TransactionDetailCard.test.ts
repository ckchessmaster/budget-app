import { fireEvent, render, waitFor } from '@testing-library/svelte';
import { describe, expect, it, vi } from 'vitest';
import TransactionDetailCard from './TransactionDetailCard.svelte';
import type { CategoryBudget } from '$lib/types/budget';
import type { AccountItem, TransactionItemModel } from '$lib/types/transaction';

const account: AccountItem = {
	id: 'acct-1',
	name: 'CHRISCHECKING (1602)',
	type: 'checking',
	currentBalance: 2891.1,
	isOnBudget: true,
	cardMode: 'PAY_IN_FULL',
	aprPercentage: 0,
	externalAccountId: 'ext-1',
	initialDebtBaseline: 0,
	statementDueDay: null,
	debtPayoffCategoryId: null
};

const categories: CategoryBudget[] = [
	{
		id: 'cat-1',
		name: 'Groceries',
		groupName: 'Essentials',
		rolloverMode: 'SWEEP',
		sweepTargetCategoryId: null,
		targetAmount: null,
		targetDate: null,
		targetCadence: 'NONE',
		sortOrder: 0,
		originalPlanned: 500,
		effectiveTarget: 500,
		spentCleared: 50,
		spentPending: 0,
		remaining: 450
	},
	{
		id: 'cat-2',
		name: 'Household',
		groupName: 'Home',
		rolloverMode: 'SWEEP',
		sweepTargetCategoryId: null,
		targetAmount: null,
		targetDate: null,
		targetCadence: 'NONE',
		sortOrder: 1,
		originalPlanned: 200,
		effectiveTarget: 200,
		spentCleared: 0,
		spentPending: 0,
		remaining: 200
	}
];

const transaction: TransactionItemModel = {
	id: 'tx-100',
	accountId: 'acct-1',
	origin: 'BANK_FEED',
	status: 'POSTED',
	amount: -45.67,
	merchantRaw: 'TRADER JOES #123 SEATTLE WA',
	merchantClean: "Trader Joe's",
	categoryId: 'cat-1',
	transactionDate: '2026-11-20',
	userNotes: 'Weekly groceries',
	isCleared: true,
	isTransfer: false
};

describe('TransactionDetailCard', () => {
	it('renders full transaction details and saves edited merchant, category, and notes', async () => {
		const onUpdateTransaction = vi.fn().mockResolvedValue(undefined);
		const onClose = vi.fn();

		const { getByText, getByLabelText, getByRole } = render(TransactionDetailCard, {
			props: {
				transaction,
				account,
				categories,
				variant: 'panel',
				onClose,
				onUpdateTransaction
			}
		});

		expect(getByText("Trader Joe's")).toBeTruthy();
		expect(getByText(/Original: TRADER JOES #123 SEATTLE WA/)).toBeTruthy();
		expect(getByText('-$45.67')).toBeTruthy();
		expect(getByText('Nov 20, 2026')).toBeTruthy();
		expect(getByText('CHRISCHECKING (1602)')).toBeTruthy();
		expect(getByText('Bank Sync')).toBeTruthy();

		await fireEvent.input(getByLabelText('Merchant'), { target: { value: 'Trader Joes Market' } });
		await fireEvent.input(getByLabelText('Notes'), { target: { value: 'Snacks' } });
		await fireEvent.change(getByLabelText('Category'), { target: { value: '' } });
		await fireEvent.click(getByRole('button', { name: 'Save Changes' }));

		await waitFor(() => {
			expect(onUpdateTransaction).toHaveBeenCalledWith('tx-100', {
				merchantClean: 'Trader Joes Market',
				userNotes: 'Snacks',
				clearCategory: true,
				clearPlannedIncome: true
			});
		});
	});

	it('requires confirmation before deleting and closes after delete', async () => {
		const onDeleteTransaction = vi.fn().mockResolvedValue(undefined);
		const onClose = vi.fn();

		const { getByTestId } = render(TransactionDetailCard, {
			props: {
				transaction,
				account,
				categories,
				variant: 'modal',
				onClose,
				onDeleteTransaction
			}
		});

		await fireEvent.click(getByTestId('tx-detail-delete-btn'));
		expect(onDeleteTransaction).not.toHaveBeenCalled();

		await fireEvent.click(getByTestId('tx-detail-confirm-delete'));
		await waitFor(() => {
			expect(onDeleteTransaction).toHaveBeenCalledWith('tx-100');
			expect(onClose).toHaveBeenCalledTimes(1);
		});
	});

	it('splits a transaction across multiple categories and supports unsplitting', async () => {
		const onReplaceSplits = vi.fn().mockResolvedValue(undefined);

		const { getByTestId, getByLabelText } = render(TransactionDetailCard, {
			props: {
				transaction,
				account,
				categories,
				variant: 'panel',
				onClose: vi.fn(),
				onReplaceSplits
			}
		});

		await fireEvent.click(getByTestId('tx-detail-split-btn'));
		expect(getByTestId('tx-split-editor')).toBeTruthy();

		await fireEvent.change(getByLabelText('Split 1 category'), { target: { value: 'cat-1' } });
		await fireEvent.input(getByLabelText('Split 1 amount'), { target: { value: '30.00' } });
		await fireEvent.change(getByLabelText('Split 2 category'), { target: { value: 'cat-2' } });
		await fireEvent.click(getByTestId('split-fill-remaining-1'));
		await fireEvent.input(getByLabelText('Split 2 note'), { target: { value: 'Soap' } });

		await fireEvent.click(getByTestId('split-save-btn'));
		await waitFor(() => {
			expect(onReplaceSplits).toHaveBeenCalledWith('tx-100', {
				splits: [
					{ amount: -30, categoryId: 'cat-1', userNotes: '' },
					{ amount: -15.67, categoryId: 'cat-2', userNotes: 'Soap' }
				]
			});
		});

		// Re-render as an already split transaction and test Unsplit
		const splitTx: TransactionItemModel = {
			...transaction,
			categoryId: null,
			splits: [
				{ id: 's-1', amount: -30, categoryId: 'cat-1', userNotes: null },
				{ id: 's-2', amount: -15.67, categoryId: 'cat-2', userNotes: 'Soap' }
			]
		};
		const unsplitView = render(TransactionDetailCard, {
			props: {
				transaction: splitTx,
				account,
				categories,
				initialSplitMode: true,
				onClose: vi.fn(),
				onReplaceSplits
			}
		});

		await fireEvent.click(unsplitView.getByTestId('split-unsplit-btn'));
		await waitFor(() => {
			expect(onReplaceSplits).toHaveBeenLastCalledWith('tx-100', {
				splits: [],
				categoryId: 'cat-1',
				plannedIncomeId: null
			});
		});
	});

	it('renders rule attribution, Approve button, suggestions, and Create Rule button', async () => {
		const onApproveReview = vi.fn().mockResolvedValue(undefined);
		const onUpdateTransaction = vi.fn().mockResolvedValue(undefined);
		const onCreateRule = vi.fn();

		const { getByTestId } = render(TransactionDetailCard, {
			props: {
				transaction: {
					...transaction,
					categorizedBy: 'RULE',
					needsReview: true
				},
				account,
				categories,
				suggestions: [
					{
						targetType: 'CATEGORY',
						categoryId: 'cat-2',
						name: 'Household',
						score: 0.92,
						reason: 'Similar amount'
					}
				],
				onClose: vi.fn(),
				onApproveReview,
				onUpdateTransaction,
				onCreateRule
			}
		});

		expect(getByTestId('tx-detail-auto-badge').textContent).toContain('Auto-categorized by rule');
		await fireEvent.click(getByTestId('tx-detail-approve-btn'));
		await waitFor(() => expect(onApproveReview).toHaveBeenCalledWith('tx-100'));

		expect(getByTestId('tx-detail-suggestion-chip').textContent).toContain('Household');
		expect(getByTestId('tx-detail-suggestion-chip').textContent).toContain('Similar amount');
		await fireEvent.click(getByTestId('tx-detail-suggestion-chip'));
		await waitFor(() =>
			expect(onUpdateTransaction).toHaveBeenCalledWith('tx-100', {
				categoryId: 'cat-2',
				clearPlannedIncome: true
			})
		);

		await fireEvent.click(getByTestId('tx-detail-create-rule-btn'));
		expect(onCreateRule).toHaveBeenCalledTimes(1);
	});
});
