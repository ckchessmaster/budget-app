import { fireEvent, render, waitFor } from '@testing-library/svelte';
import { describe, expect, it, vi } from 'vitest';
import CatchUpModal from './CatchUpModal.svelte';
import type { CategoryBudget } from '$lib/types/budget';
import type { AccountItem, TransactionItemModel } from '$lib/types/transaction';
import {
	isCatchUpTransaction,
	isCategorizableTransaction,
	suggestCategoriesForTransaction
} from '$lib/utils/transactionReview';

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
		id: 'cat-rent',
		name: 'Rent',
		groupName: 'Essentials',
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
		id: 'cat-groc',
		name: 'Groceries',
		groupName: 'Essentials',
		rolloverMode: 'SWEEP',
		sweepTargetCategoryId: null,
		targetAmount: null,
		targetDate: null,
		targetCadence: 'NONE',
		sortOrder: 2,
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
		sortOrder: 3,
		originalPlanned: 150,
		effectiveTarget: 150,
		spentCleared: 20,
		spentPending: 0,
		remaining: 130
	},
	{
		id: 'cat-subs',
		name: 'Subscriptions',
		groupName: 'Lifestyle',
		rolloverMode: 'SWEEP',
		sweepTargetCategoryId: null,
		targetAmount: null,
		targetDate: null,
		targetCadence: 'NONE',
		sortOrder: 4,
		originalPlanned: 40,
		effectiveTarget: 40,
		spentCleared: 15,
		spentPending: 0,
		remaining: 25
	}
];

const catchUpItems: TransactionItemModel[] = [
	{
		id: 'tx-1',
		accountId: 'acct-checking',
		origin: 'BANK_FEED',
		status: 'POSTED',
		amount: -24.5,
		merchantRaw: 'BLUE BOTTLE COFFEE',
		merchantClean: 'Blue Bottle Coffee',
		categoryId: null,
		transactionDate: '2026-11-18',
		isCleared: true,
		isTransfer: false
	},
	{
		id: 'tx-2',
		accountId: 'acct-checking',
		origin: 'BANK_FEED',
		status: 'PENDING',
		amount: -64.12,
		merchantRaw: 'TRADER JOES #102',
		categoryId: null,
		transactionDate: '2026-11-19',
		isCleared: false,
		isTransfer: false
	}
];

const historicalTransactions: TransactionItemModel[] = [
	...catchUpItems,
	{
		id: 'tx-hist-1',
		accountId: 'acct-checking',
		origin: 'BANK_FEED',
		status: 'POSTED',
		amount: -6.5,
		merchantRaw: 'Blue Bottle Coffee',
		categoryId: 'cat-dining',
		transactionDate: '2026-11-05',
		isCleared: true,
		isTransfer: false
	},
	{
		id: 'tx-hist-2',
		accountId: 'acct-checking',
		origin: 'BANK_FEED',
		status: 'POSTED',
		amount: -7.25,
		merchantRaw: 'Blue Bottle Coffee',
		categoryId: 'cat-dining',
		transactionDate: '2026-11-10',
		isCleared: true,
		isTransfer: false
	}
];

function baseProps(overrides: Partial<Record<string, unknown>> = {}) {
	return {
		isOpen: true,
		initialMode: 'intro' as const,
		items: catchUpItems,
		allTransactions: historicalTransactions,
		accounts,
		categories,
		onCategorizeTransaction: vi.fn().mockResolvedValue(undefined),
		onClose: vi.fn(),
		...overrides
	};
}

describe('CatchUpModal', () => {
	it('renders Time to catch up intro view, supports Skip for Now, and transitions to Swipe Stack on Catch Up Now', async () => {
		const onClose = vi.fn();
		const { getByText, getAllByTestId, getByTestId, queryByTestId } = render(CatchUpModal, {
			props: baseProps({ onClose })
		});

		expect(getByText('Time to catch up')).toBeTruthy();
		expect(getAllByTestId('catchup-intro-item')).toHaveLength(2);
		expect(getByText('Blue Bottle Coffee')).toBeTruthy();
		expect(getByText('TRADER JOES #102')).toBeTruthy();
		expect(getByTestId('catchup-badge-new').textContent?.trim()).toBe('New');
		expect(getByTestId('catchup-badge-pending').textContent?.trim()).toBe('Pending');
		expect(getByText('Nov 18')).toBeTruthy();
		expect(getByText('Nov 19')).toBeTruthy();

		await fireEvent.click(getByTestId('catchup-skip-for-now-btn'));
		expect(onClose).toHaveBeenCalledTimes(1);

		await fireEvent.click(getByTestId('catchup-now-btn'));
		expect(getByTestId('catchup-stack-progress').textContent?.trim()).toBe('Catch Up · 1 of 2');
		expect(getByTestId('catchup-stack-card').textContent).toContain('Blue Bottle Coffee');
		expect(getByTestId('catchup-stack-card').textContent).toContain('-$24.50');
		expect(queryByTestId('catchup-intro-item')).toBeNull();
	});

	it('prioritizes merchant-history suggestions, assigns categories sequentially, and auto-closes when queue reaches zero', async () => {
		const onCategorizeTransaction = vi.fn().mockResolvedValue(undefined);
		const onClose = vi.fn();

		const { getAllByTestId, getByTestId, getByText } = render(CatchUpModal, {
			props: baseProps({
				initialMode: 'stack',
				onCategorizeTransaction,
				onClose
			})
		});

		expect(getByTestId('catchup-stack-progress').textContent?.trim()).toBe('Catch Up · 1 of 2');
		const chips = getAllByTestId('catchup-category-chip');
		// Blue Bottle Coffee matched Dining Out in history, so Dining Out is first
		expect(chips[0].textContent?.trim()).toBe('Dining Out');

		await fireEvent.click(chips[0]);
		await waitFor(() =>
			expect(onCategorizeTransaction).toHaveBeenCalledWith('tx-1', 'cat-dining')
		);

		// Advances automatically to the 2nd item (now 1 of 1 remaining)
		await waitFor(() =>
			expect(getByTestId('catchup-stack-progress').textContent?.trim()).toBe('Catch Up · 1 of 1')
		);
		expect(getByText('TRADER JOES #102')).toBeTruthy();
		expect(getByTestId('catchup-badge-pending')).toBeTruthy();

		// Categorize final item -> calls onClose automatically
		const nextChips = getAllByTestId('catchup-category-chip');
		await fireEvent.click(nextChips[0]);
		await waitFor(() => expect(onCategorizeTransaction).toHaveBeenCalledTimes(2));
		await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
	});

	it('reveals full category list via More..., filters by search, and assigns selected category', async () => {
		const onCategorizeTransaction = vi.fn().mockResolvedValue(undefined);

		const { getByTestId, getByLabelText, getAllByTestId, queryByTestId } = render(
			CatchUpModal,
			{
				props: baseProps({
					initialMode: 'stack',
					onCategorizeTransaction
				})
			}
		);

		expect(queryByTestId('catchup-all-categories')).toBeNull();
		await fireEvent.click(getByTestId('catchup-more-btn'));
		expect(getByTestId('catchup-all-categories')).toBeTruthy();

		await fireEvent.input(getByLabelText('Search categories'), {
			target: { value: 'Subscr' }
		});

		const options = getAllByTestId('catchup-all-category-option');
		expect(options).toHaveLength(1);
		expect(options[0].textContent).toContain('Subscriptions');

		await fireEvent.click(options[0]);
		await waitFor(() =>
			expect(onCategorizeTransaction).toHaveBeenCalledWith('tx-1', 'cat-subs')
		);
	});

	it('supports Skip -> cycling and Finish later dismissal', async () => {
		const onClose = vi.fn();
		const { getByTestId } = render(CatchUpModal, {
			props: baseProps({
				initialMode: 'stack',
				onClose
			})
		});

		expect(getByTestId('catchup-stack-progress').textContent?.trim()).toBe('Catch Up · 1 of 2');
		expect(getByTestId('catchup-stack-card').textContent).toContain('Blue Bottle Coffee');

		await fireEvent.click(getByTestId('catchup-skip-btn'));
		expect(getByTestId('catchup-stack-progress').textContent?.trim()).toBe('Catch Up · 2 of 2');
		expect(getByTestId('catchup-stack-card').textContent).toContain('TRADER JOES #102');

		// Wraps around back to 1 of 2
		await fireEvent.click(getByTestId('catchup-skip-btn'));
		expect(getByTestId('catchup-stack-progress').textContent?.trim()).toBe('Catch Up · 1 of 2');
		expect(getByTestId('catchup-stack-card').textContent).toContain('Blue Bottle Coffee');

		await fireEvent.click(getByTestId('catchup-finish-later-btn'));
		expect(onClose).toHaveBeenCalledTimes(1);
	});

	it('keeps card open and displays alert error when categorization fails', async () => {
		const onCategorizeTransaction = vi.fn().mockRejectedValue(new Error('Network timeout'));
		const onClose = vi.fn();

		const { getAllByTestId, getByRole, getByTestId } = render(CatchUpModal, {
			props: baseProps({
				initialMode: 'stack',
				onCategorizeTransaction,
				onClose
			})
		});

		await fireEvent.click(getAllByTestId('catchup-category-chip')[0]);
		await waitFor(() => expect(getByRole('alert').textContent).toContain('Network timeout'));
		expect(onClose).not.toHaveBeenCalled();
		expect(getByTestId('catchup-stack-card').textContent).toContain('Blue Bottle Coffee');
	});

	it('closes on Escape key press', async () => {
		const onClose = vi.fn();
		render(CatchUpModal, {
			props: baseProps({ onClose })
		});

		await fireEvent.keyDown(window, { key: 'Escape' });
		expect(onClose).toHaveBeenCalledTimes(1);
	});
});

describe('transactionReview Catch Up helpers', () => {
	const accountById = new Map(accounts.map((a) => [a.id, a]));

	it('identifies categorizable and Catch Up eligible transactions accurately', () => {
		const postedUncat = catchUpItems[0];
		const pendingUncat = catchUpItems[1];
		const pendingCategorized: TransactionItemModel = {
			...pendingUncat,
			id: 'tx-pending-cat',
			categoryId: 'cat-groc'
		};
		const offBudgetUncat: TransactionItemModel = {
			...postedUncat,
			id: 'tx-offbudget',
			accountId: 'acct-401k'
		};
		const queuedOffline: TransactionItemModel = {
			...pendingUncat,
			id: 'offline-1',
			syncStatus: 'QUEUED'
		};
		const transferTx: TransactionItemModel = {
			...postedUncat,
			id: 'tx-transfer',
			isTransfer: true
		};

		expect(isCatchUpTransaction(postedUncat, accountById)).toBe(true);
		expect(isCatchUpTransaction(pendingUncat, accountById)).toBe(true);
		expect(isCatchUpTransaction(pendingCategorized, accountById)).toBe(false);
		expect(isCatchUpTransaction(offBudgetUncat, accountById)).toBe(false);
		expect(isCatchUpTransaction(queuedOffline, accountById)).toBe(false);
		expect(isCatchUpTransaction(transferTx, accountById)).toBe(false);

		expect(isCategorizableTransaction(pendingCategorized, accountById)).toBe(true);
		expect(isCategorizableTransaction(queuedOffline, accountById)).toBe(false);
		expect(isCategorizableTransaction(offBudgetUncat, accountById)).toBe(false);
	});

	it('suggests categories from keyword hints and historical matches', () => {
		const traderJoesSuggestions = suggestCategoriesForTransaction(
			catchUpItems[1],
			categories,
			[],
			3
		);
		expect(traderJoesSuggestions).toHaveLength(3);
		expect(traderJoesSuggestions[0].name).toBe('Groceries');

		const coffeeSuggestions = suggestCategoriesForTransaction(
			catchUpItems[0],
			categories,
			historicalTransactions,
			4
		);
		expect(coffeeSuggestions[0].name).toBe('Dining Out');
	});
});
