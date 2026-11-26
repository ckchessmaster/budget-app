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

	it('opens inline quick category picker on pill click, fires onSelectTransaction on row click, and categorizes transaction', async () => {
		const onCategorizeTransaction = vi.fn().mockResolvedValue(undefined);
		const onSelectTransaction = vi.fn();

		const { getByText, getByTestId, queryByTestId, getByLabelText, getAllByTestId } = render(
			TransactionsPanel,
			{
				props: baseProps({ onCategorizeTransaction, onSelectTransaction })
			}
		);

		// Clicking the row (merchant) opens Transaction Details instead of category picker
		await fireEvent.click(getByText('Tiktok Shop'));
		expect(onSelectTransaction).toHaveBeenCalledWith('tx-new');
		expect(queryByTestId('quick-category-picker')).toBeNull();

		// Clicking the category pill opens inline quick category picker
		await fireEvent.click(getByLabelText('Category for Tiktok Shop'));
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

	it('supports overflow row menu for viewing details, splitting, and deleting a transaction', async () => {
		const onSelectTransaction = vi.fn();
		const onDeleteTransaction = vi.fn().mockResolvedValue(undefined);

		const { getByLabelText, getByTestId } = render(TransactionsPanel, {
			props: baseProps({ onSelectTransaction, onDeleteTransaction })
		});

		// Open overflow menu for Tiktok Shop -> View details & Split transaction
		await fireEvent.click(getByLabelText('More actions for Tiktok Shop'));
		expect(getByTestId('tx-row-menu')).toBeTruthy();
		await fireEvent.click(getByTestId('tx-menu-details'));
		expect(onSelectTransaction).toHaveBeenCalledWith('tx-new');

		await fireEvent.click(getByLabelText('More actions for Tiktok Shop'));
		await fireEvent.click(getByTestId('tx-menu-split'));
		expect(onSelectTransaction).toHaveBeenCalledWith('tx-new', { openSplit: true });

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
		await fireEvent.click(getByLabelText('Category'));
		await fireEvent.click(
			document.querySelector('[data-testid="qa-cat-list"] [data-testid="qa-cat-option"]')!
		);
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

		const { getByRole, getByText, getByTestId, getByLabelText, queryByText } = render(TransactionsPanel, {
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

		// Assign Tiktok Shop (an expense) to Unplanned Income via the "Show income options" override
		await fireEvent.click(getByLabelText('Category for Tiktok Shop'));
		expect(queryByText('Unplanned Income')).toBeNull();
		await fireEvent.click(getByTestId('quick-picker-show-income-btn'));
		await fireEvent.click(getByTestId('quick-income-option'));
		await waitFor(() =>
			expect(onAssignTransactionToIncome).toHaveBeenCalledWith('tx-new', 'inc-unplanned')
		);

		// All tab renders Income pill
		await fireEvent.click(getByRole('tab', { name: 'All (4)' }));
		expect(getByText('Income: Unplanned Income')).toBeTruthy();
	});

	it('renders top suggestions, shows income first on inflows, shows post-categorize bar, and supports Approve/Auto badges', async () => {
		const incomes: PlannedIncome[] = [
			{
				id: 'inc-pay',
				workspaceId: 'ws-1',
				yearMonth: '2026-11-01',
				name: 'Primary Paycheck',
				amount: 2500,
				createdAt: '2026-11-01T00:00:00Z'
			}
		];
		const onCategorizeTransaction = vi.fn().mockResolvedValue(undefined);
		const onAssignTransactionToIncome = vi.fn().mockResolvedValue(undefined);
		const onApproveReview = vi.fn().mockResolvedValue(undefined);
		const onCreateRule = vi.fn();
		const onApplySimilar = vi.fn().mockResolvedValue(1);

		const extraTxs: TransactionItemModel[] = [
			...transactions,
			{
				id: 'tx-new-2',
				accountId: 'acct-checking',
				origin: 'BANK_FEED',
				status: 'POSTED',
				amount: -19.0,
				merchantRaw: 'Tiktok Shop Order 2',
				categoryId: null,
				transactionDate: '2026-11-19',
				isCleared: true,
				isTransfer: false
			},
			{
				id: 'tx-inflow',
				accountId: 'acct-checking',
				origin: 'BANK_FEED',
				status: 'POSTED',
				amount: 2500,
				merchantRaw: 'ACME PAYROLL',
				categoryId: null,
				transactionDate: '2026-11-19',
				isCleared: true,
				isTransfer: false
			},
			{
				id: 'tx-rule-review',
				accountId: 'acct-checking',
				origin: 'BANK_FEED',
				status: 'POSTED',
				amount: -12.0,
				merchantRaw: 'Starbucks',
				categoryId: 'cat-dining',
				categorizedBy: 'RULE',
				needsReview: true,
				transactionDate: '2026-11-19',
				isCleared: true,
				isTransfer: false
			},
			{
				id: 'tx-rule-auto',
				accountId: 'acct-checking',
				origin: 'BANK_FEED',
				status: 'POSTED',
				amount: -15.99,
				merchantRaw: 'Netflix',
				categoryId: 'cat-dining',
				categorizedBy: 'RULE',
				needsReview: false,
				transactionDate: '2026-11-15',
				isCleared: true,
				isTransfer: false
			}
		];

		const { getByTestId, getByLabelText, getByRole, getByText } = render(TransactionsPanel, {
			props: baseProps({
				transactions: extraTxs,
				incomes,
				suggestionsByTxId: {
					'tx-new': [
						{
							targetType: 'CATEGORY',
							categoryId: 'cat-dining',
							name: 'Dining Out',
							score: 0.9,
							reason: 'Used 3×'
						}
					]
				},
				onCategorizeTransaction,
				onAssignTransactionToIncome,
				onApproveReview,
				onCreateRule,
				onApplySimilar
			})
		});

		// Rule-flagged transaction shows Review badge and Approve button
		expect(getByTestId('tx-badge-review')).toBeTruthy();
		await fireEvent.click(getByTestId('tx-approve-review-btn'));
		await waitFor(() => expect(onApproveReview).toHaveBeenCalledWith('tx-rule-review'));

		// Inflow shows Income section immediately without needing Show income toggle
		await fireEvent.click(getByLabelText('Category for ACME PAYROLL'));
		expect(getByTestId('quick-picker-income-section')).toBeTruthy();

		// Expense opens Suggested section and triggers post-categorize bar
		await fireEvent.click(getByLabelText('Category for Tiktok Shop'));
		expect(getByTestId('quick-picker-suggestions').textContent).toContain('Dining Out');
		expect(getByTestId('quick-picker-suggestions').textContent).toContain('Used 3×');

		await fireEvent.click(getByTestId('quick-suggestion-option'));
		await waitFor(() => expect(onCategorizeTransaction).toHaveBeenCalledWith('tx-new', 'cat-dining'));

		const actionBar = getByTestId('post-categorize-action-bar');
		expect(actionBar.textContent).toContain('Apply to 1 other');
		await fireEvent.click(getByTestId('post-cat-apply-similar-btn'));
		await waitFor(() => expect(onApplySimilar).toHaveBeenCalledTimes(1));

		await fireEvent.click(getByTestId('post-cat-always-rule-btn'));
		expect(onCreateRule).toHaveBeenCalledTimes(1);

		// Auto-categorized without review shows Auto badge in All tab
		await fireEvent.click(getByRole('tab', { name: /All/ }));
		expect(getByText('Netflix')).toBeTruthy();
		expect(getByTestId('tx-badge-auto')).toBeTruthy();
	});

	it('shows cross-month To Review items, budget month warning badge, and ignore-review actions', async () => {
		const onUpdateTransaction = vi.fn().mockResolvedValue(undefined);
		const onIgnoreReviewBefore = vi.fn().mockResolvedValue(1);

		const oldToReview: TransactionItemModel = {
			id: 'tx-old-unreviewed',
			accountId: 'acc-1',
			origin: 'BANK_FEED',
			status: 'POSTED',
			amount: -45.0,
			merchantRaw: 'Old Hardware Store',
			categoryId: null,
			transactionDate: '2026-10-28',
			isCleared: true,
			isTransfer: false
		};

		const overriddenTx: TransactionItemModel = {
			id: 'tx-overridden',
			accountId: 'acc-1',
			origin: 'BANK_FEED',
			status: 'POSTED',
			amount: -120.0,
			merchantRaw: 'Late Oct Utility',
			categoryId: 'cat-dining',
			transactionDate: '2026-10-30',
			budgetMonth: '2026-11',
			isCleared: true,
			isTransfer: false
		};

		const { getByText, getByTestId, getByRole } = render(TransactionsPanel, {
			props: {
				...baseProps(),
				currentMonth: '2026-11',
				transactions: [overriddenTx],
				toReviewTransactions: [oldToReview],
				onUpdateTransaction,
				onIgnoreReviewBefore
			}
		});

		// To Review tab shows cross-month unreviewed item and bulk ignore banner
		expect(getByText('Old Hardware Store')).toBeTruthy();
		expect(getByTestId('tx-ignore-older-banner').textContent).toContain('1 older unreviewed');

		// Confirm bulk ignore older
		await fireEvent.click(getByTestId('tx-ignore-older-btn'));
		await fireEvent.click(getByTestId('tx-confirm-ignore-older-btn'));
		await waitFor(() => expect(onIgnoreReviewBefore).toHaveBeenCalledWith('2026-11'));

		// Per-row menu: Ignore from review and Move to current month
		await fireEvent.click(getByTestId('tx-row-menu-btn'));
		await fireEvent.click(getByTestId('tx-menu-ignore-review'));
		await waitFor(() =>
			expect(onUpdateTransaction).toHaveBeenCalledWith('tx-old-unreviewed', { reviewIgnored: true })
		);

		// All tab shows budget month override warning badge
		await fireEvent.click(getByRole('tab', { name: /All/ }));
		expect(getByTestId('tx-badge-budget-month').textContent).toContain('Budget: 2026-11');
	});
});
