import { fireEvent, render, waitFor } from '@testing-library/svelte';
import { describe, expect, it, vi } from 'vitest';
import TransactionsPanel from './TransactionsPanel.svelte';
import type { CategoryBudget } from '$lib/types/budget';
import type { AccountItem, TransactionItemModel } from '$lib/types/transaction';

const accounts: AccountItem[] = [
	{
		id: 'acct-checking',
		name: 'Main Checking',
		type: 'checking',
		currentBalance: 3500,
		isOnBudget: true,
		cardMode: 'PAY_IN_FULL',
		aprPercentage: 0,
		externalAccountId: null,
		initialDebtBaseline: 0,
		statementDueDay: null,
		debtPayoffCategoryId: null
	},
	{
		id: 'acct-401k',
		name: '401k Tracking',
		type: 'investment',
		currentBalance: 10000,
		isOnBudget: false,
		cardMode: 'PAY_IN_FULL',
		aprPercentage: 0,
		externalAccountId: null,
		initialDebtBaseline: 0,
		statementDueDay: null,
		debtPayoffCategoryId: null
	}
];

const categories: CategoryBudget[] = [
	{
		id: 'cat-groc',
		name: 'Groceries',
		groupName: 'Essentials',
		rolloverMode: 'SWEEP',
		sweepTargetCategoryId: null,
		targetAmount: null,
		targetDate: null,
		targetCadence: 'NONE',
		sortOrder: 1,
		originalPlanned: 500,
		effectiveTarget: 500,
		spentCleared: 50,
		spentPending: 0,
		remaining: 450
	}
];

const transactions: TransactionItemModel[] = [
	{
		id: 'tx-new',
		accountId: 'acct-checking',
		origin: 'BANK_FEED',
		status: 'POSTED',
		amount: -15.17,
		merchantRaw: 'Tiktok Shop',
		categoryId: null,
		transactionDate: '2026-11-18',
		isCleared: true,
		isTransfer: false
	},
	{
		id: 'tx-tracked',
		accountId: 'acct-checking',
		origin: 'BANK_FEED',
		status: 'PENDING',
		amount: -50.0,
		merchantRaw: 'Trader Joes',
		categoryId: 'cat-groc',
		transactionDate: '2026-11-17',
		isCleared: false,
		isTransfer: false
	},
	{
		id: 'tx-offbudget',
		accountId: 'acct-401k',
		origin: 'MANUAL_OFFLINE',
		status: 'POSTED',
		amount: 250.0,
		merchantRaw: '401k Contribution',
		categoryId: null,
		transactionDate: '2026-11-16',
		isCleared: true,
		isTransfer: false
	}
];

describe('TransactionsPanel', () => {
	it('separates New (uncategorized on-budget) from Tracked/Pending and supports inline categorization', async () => {
		const onCategorizeTransaction = vi.fn().mockResolvedValue(undefined);
		const onCreateTransaction = vi.fn().mockResolvedValue(undefined);

		const { getByRole, getAllByTestId, getByLabelText, queryByText, getByText } = render(
			TransactionsPanel,
			{
				props: {
					currentMonth: '2026-11',
					transactions,
					queuedTransactions: [],
					accounts,
					categories,
					isOffline: false,
					syncing: false,
					activeCategoryFilterId: null,
					onClearCategoryFilter: vi.fn(),
					onCreateTransaction,
					onCategorizeTransaction,
					onDeleteTransaction: vi.fn(),
					onSyncNow: vi.fn()
				}
			}
		);

		// Default tab is New (1) -> only tx-new (off-budget is excluded from New)
		expect(getByRole('tab', { name: 'New (1)' })).toBeTruthy();
		expect(getAllByTestId('tx-panel-row')).toHaveLength(1);
		expect(getByText('Tiktok Shop')).toBeTruthy();
		expect(queryByText('Trader Joes')).toBeNull();
		expect(queryByText('401k Contribution')).toBeNull();

		// Date badge rendered without UTC timezone shift
		expect(getByLabelText('2026-11-18').textContent).toContain('18');

		// Inline categorize tx-new to Groceries
		const catSelect = getByLabelText('Category for Tiktok Shop') as HTMLSelectElement;
		await fireEvent.change(catSelect, { target: { value: 'cat-groc' } });
		await waitFor(() => expect(onCategorizeTransaction).toHaveBeenCalledWith('tx-new', 'cat-groc'));

		// Switch to Pending tab -> shows Trader Joes
		await fireEvent.click(getByRole('tab', { name: 'Pending (1)' }));
		expect(getAllByTestId('tx-panel-row')).toHaveLength(1);
		expect(getByText('Trader Joes')).toBeTruthy();

		// Switch to All and search
		await fireEvent.click(getByRole('tab', { name: 'All' }));
		expect(getAllByTestId('tx-panel-row')).toHaveLength(3);
		await fireEvent.input(getByLabelText('Search transactions'), { target: { value: '401k' } });
		expect(getAllByTestId('tx-panel-row')).toHaveLength(1);
		expect(getByText('401k Contribution')).toBeTruthy();
	});

	it('submits quick-add transaction form with signed amount', async () => {
		const onCreateTransaction = vi.fn().mockResolvedValue(undefined);

		const { getByRole, getByLabelText } = render(TransactionsPanel, {
			props: {
				currentMonth: '2026-11',
				transactions: [],
				queuedTransactions: [],
				accounts,
				categories,
				isOffline: false,
				syncing: false,
				activeCategoryFilterId: null,
				onClearCategoryFilter: vi.fn(),
				onCreateTransaction,
				onCategorizeTransaction: vi.fn(),
				onDeleteTransaction: vi.fn(),
				onSyncNow: vi.fn()
			}
		});

		await fireEvent.click(getByRole('button', { name: '+ Add' }));
		await fireEvent.input(getByLabelText('Merchant / Payee'), { target: { value: 'Whole Foods' } });
		await fireEvent.input(getByLabelText('Amount ($)'), { target: { value: '42.50' } });
		await fireEvent.change(getByLabelText('Category'), { target: { value: 'cat-groc' } });
		await fireEvent.click(getByRole('button', { name: 'Save Expense' }));

		await waitFor(() => expect(onCreateTransaction).toHaveBeenCalledTimes(1));
		expect(onCreateTransaction.mock.calls[0][0]).toMatchObject({
			accountId: 'acct-checking',
			amount: -42.5,
			merchantRaw: 'Whole Foods',
			categoryId: 'cat-groc'
		});
	});
});
