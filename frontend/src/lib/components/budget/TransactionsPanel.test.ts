import { fireEvent, render, waitFor } from '@testing-library/svelte';
import { describe, expect, it, vi } from 'vitest';
import TransactionsPanel from './TransactionsPanel.svelte';
import type { CategoryBudget, PlannedIncome } from '$lib/types/budget';
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
	},
	{
		id: 'cat-dining',
		name: 'Dining Out',
		groupName: 'Lifestyle',
		rolloverMode: 'SWEEP',
		sweepTargetCategoryId: null,
		targetAmount: null,
		targetDate: null,
		targetCadence: 'NONE',
		sortOrder: 2,
		originalPlanned: 150,
		effectiveTarget: 150,
		spentCleared: 20,
		spentPending: 0,
		remaining: 130
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
		id: 'tx-pending',
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

function baseProps(overrides: Partial<Record<string, unknown>> = {}) {
	return {
		currentMonth: '2026-11',
		transactions,
		queuedTransactions: [],
		accounts,
		categories,
		isOffline: false,
		syncing: false,
		activeCategoryFilterId: null,
		onClearCategoryFilter: vi.fn(),
		onCreateTransaction: vi.fn().mockResolvedValue(undefined),
		onCategorizeTransaction: vi.fn().mockResolvedValue(undefined),
		onUpdateTransaction: vi.fn().mockResolvedValue(undefined),
		onDeleteTransaction: vi.fn().mockResolvedValue(undefined),
		onSyncNow: vi.fn().mockResolvedValue(undefined),
		...overrides
	};
}

describe('TransactionsPanel', () => {
	it('renders combined To Review and All tabs with New/Pending badges and no inline Delete buttons', () => {
		const {
			getByRole,
			queryByRole,
			getAllByTestId,
			getByTestId,
			getByLabelText,
			queryByText,
			getByText
		} = render(TransactionsPanel, {
			props: baseProps()
		});

		expect(getByRole('tab', { name: 'To Review (2)' })).toBeTruthy();
		expect(getByRole('tab', { name: 'All (3)' })).toBeTruthy();
		expect(queryByRole('tab', { name: /^New/ })).toBeNull();
		expect(queryByRole('tab', { name: /^Tracked/ })).toBeNull();
		expect(queryByRole('tab', { name: /^Pending/ })).toBeNull();

		// To Review shows both uncategorized posted (New) and pending items, excluding off-budget posted
		expect(getAllByTestId('tx-panel-row')).toHaveLength(2);
		expect(getByText('Tiktok Shop')).toBeTruthy();
		expect(getByTestId('tx-badge-new').textContent?.trim()).toBe('New');
		expect(getByText('Trader Joes')).toBeTruthy();
		expect(getByTestId('tx-badge-pending').textContent?.trim()).toBe('Pending');
		expect(queryByText('401k Contribution')).toBeNull();

		// Date badge rendered without UTC timezone shift
		expect(getByLabelText('2026-11-18').textContent).toContain('18');

		// No inline Delete buttons visible on rows
		expect(queryByText('Delete')).toBeNull();
	});

	it('opens inline quick category picker on row click and categorizes transaction', async () => {
		const onCategorizeTransaction = vi.fn().mockResolvedValue(undefined);

		const { getByText, getByTestId, queryByTestId, getByLabelText, getAllByTestId } = render(
			TransactionsPanel,
			{
				props: baseProps({ onCategorizeTransaction })
			}
		);

		await fireEvent.click(getByText('Tiktok Shop'));
		expect(getByTestId('quick-category-picker')).toBeTruthy();

		// Filter categories inside picker
		await fireEvent.input(getByLabelText('Filter categories'), { target: { value: 'Groc' } });
		const options = getAllByTestId('quick-category-option');
		expect(options).toHaveLength(1);
		expect(options[0].textContent?.trim()).toBe('Groceries');

		await fireEvent.click(options[0]);
		await waitFor(() =>
			expect(onCategorizeTransaction).toHaveBeenCalledWith('tx-new', 'cat-groc')
		);
		await waitFor(() => expect(queryByTestId('quick-category-picker')).toBeNull());
	});

	it('supports overflow row menu for editing details and deleting a transaction', async () => {
		const onUpdateTransaction = vi.fn().mockResolvedValue(undefined);
		const onDeleteTransaction = vi.fn().mockResolvedValue(undefined);

		const { getByLabelText, getByTestId, getByRole, queryByTestId } = render(
			TransactionsPanel,
			{
				props: baseProps({ onUpdateTransaction, onDeleteTransaction })
			}
		);

		// Open overflow menu for Tiktok Shop -> Edit details
		await fireEvent.click(getByLabelText('More actions for Tiktok Shop'));
		expect(getByTestId('tx-row-menu')).toBeTruthy();
		await fireEvent.click(getByTestId('tx-menu-edit'));

		expect(getByTestId('tx-edit-form')).toBeTruthy();
		await fireEvent.input(getByLabelText('Edit merchant'), { target: { value: 'TikTok Shop Clean' } });
		await fireEvent.input(getByLabelText('Edit notes'), { target: { value: 'Birthday gift' } });
		await fireEvent.click(getByRole('button', { name: 'Save changes' }));

		await waitFor(() =>
			expect(onUpdateTransaction).toHaveBeenCalledWith('tx-new', {
				merchantClean: 'TikTok Shop Clean',
				userNotes: 'Birthday gift',
				clearCategory: true
			})
		);
		await waitFor(() => expect(queryByTestId('tx-edit-form')).toBeNull());

		// Open overflow menu for Trader Joes -> Delete transaction -> Confirm Delete
		await fireEvent.click(getByLabelText('More actions for Trader Joes'));
		await fireEvent.click(getByTestId('tx-menu-delete'));
		await fireEvent.click(getByTestId('tx-menu-confirm-delete'));

		await waitFor(() => expect(onDeleteTransaction).toHaveBeenCalledWith('tx-pending'));
	});

	it('switches to All tab and filters by merchant or account search', async () => {
		const { getByRole, getAllByTestId, getByLabelText, getByText } = render(TransactionsPanel, {
			props: baseProps()
		});

		await fireEvent.click(getByRole('tab', { name: 'All (3)' }));
		expect(getAllByTestId('tx-panel-row')).toHaveLength(3);

		await fireEvent.input(getByLabelText('Search transactions'), { target: { value: '401k' } });
		expect(getAllByTestId('tx-panel-row')).toHaveLength(1);
		expect(getByText('401k Contribution')).toBeTruthy();

		await fireEvent.input(getByLabelText('Search transactions'), {
			target: { value: 'Main Checking' }
		});
		expect(getAllByTestId('tx-panel-row')).toHaveLength(2);
	});

	it('submits quick-add transaction form with signed amount', async () => {
		const onCreateTransaction = vi.fn().mockResolvedValue(undefined);

		const { getByRole, getByLabelText } = render(TransactionsPanel, {
			props: baseProps({
				transactions: [],
				onCreateTransaction
			})
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

	it('excludes income-assigned transactions from To Review and supports quick income assignment', async () => {
		const incomes: PlannedIncome[] = [
			{
				id: 'inc-unplanned',
				workspaceId: 'ws-1',
				yearMonth: '2026-11-01',
				name: 'Unplanned Income',
				amount: 0,
				createdAt: '2026-11-01T00:00:00Z'
			}
		];
		const onAssignTransactionToIncome = vi.fn().mockResolvedValue(undefined);

		const txWithIncome: TransactionItemModel[] = [
			...transactions,
			{
				id: 'tx-venmo',
				accountId: 'acct-checking',
				origin: 'BANK_FEED',
				status: 'POSTED',
				amount: 86,
				merchantRaw: 'Transfer From Venmo',
				categoryId: null,
				plannedIncomeId: 'inc-unplanned',
				transactionDate: '2026-11-20',
				isCleared: true,
				isTransfer: false
			}
		];

		const { getByRole, getByText, getByTestId, queryByText } = render(TransactionsPanel, {
			props: baseProps({
				transactions: txWithIncome,
				incomes,
				onAssignTransactionToIncome
			})
		});

		// Income-assigned posted transaction is NOT in To Review (still 2)
		expect(getByRole('tab', { name: 'To Review (2)' })).toBeTruthy();
		expect(getByRole('tab', { name: 'All (4)' })).toBeTruthy();
		expect(queryByText('Transfer From Venmo')).toBeNull();

		// Assign Tiktok Shop to Unplanned Income via quick picker
		await fireEvent.click(getByText('Tiktok Shop'));
		await fireEvent.click(getByTestId('quick-income-option'));
		await waitFor(() =>
			expect(onAssignTransactionToIncome).toHaveBeenCalledWith('tx-new', 'inc-unplanned')
		);

		// All tab renders Income pill
		await fireEvent.click(getByRole('tab', { name: 'All (4)' }));
		expect(getByText('Income: Unplanned Income')).toBeTruthy();
	});
});
