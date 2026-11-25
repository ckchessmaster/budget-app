import { fireEvent, render, waitFor } from '@testing-library/svelte';
import { describe, expect, it, vi, beforeEach } from 'vitest';
import CategorizationRulesSection from './CategorizationRulesSection.svelte';
import type { CategoryBudget } from '$lib/types/budget';
import type { CategorizationRule } from '$lib/types/categorization';
import type { AccountItem } from '$lib/types/transaction';

const mockApi = vi.hoisted(() => ({
	fetchCategorizationRules: vi.fn(),
	createCategorizationRule: vi.fn(),
	updateCategorizationRule: vi.fn(),
	deleteCategorizationRule: vi.fn(),
	reorderCategorizationRules: vi.fn(),
	applyCategorizationRule: vi.fn()
}));

vi.mock('$lib/api/categorization', () => mockApi);

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
		sortOrder: 0,
		originalPlanned: 400,
		effectiveTarget: 400,
		spentCleared: 50,
		spentPending: 0,
		remaining: 350
	}
];

const accounts: AccountItem[] = [
	{
		id: 'acct-checking',
		name: 'Checking',
		type: 'checking',
		currentBalance: 2000,
		isOnBudget: true,
		cardMode: 'PAY_IN_FULL',
		aprPercentage: 0,
		externalAccountId: null,
		initialDebtBaseline: 0,
		statementDueDay: null,
		debtPayoffCategoryId: null
	}
];

const sampleRules: CategorizationRule[] = [
	{
		id: 'rule-1',
		workspaceId: 'ws-1',
		priority: 0,
		matchType: 'CONTAINS',
		merchantPattern: 'Trader Joe',
		accountId: null,
		direction: 'EXPENSE',
		minAmount: null,
		maxAmount: null,
		categoryId: 'cat-groc',
		plannedIncomeName: null,
		renameMerchant: "Trader Joe's",
		requireReview: false,
		isActive: true,
		createdAt: '2026-11-01T00:00:00Z',
		updatedAt: '2026-11-01T00:00:00Z'
	},
	{
		id: 'rule-2',
		workspaceId: 'ws-1',
		priority: 1,
		matchType: 'EXACT',
		merchantPattern: 'ACME PAYROLL',
		accountId: 'acct-checking',
		direction: 'INFLOW',
		minAmount: null,
		maxAmount: null,
		categoryId: null,
		plannedIncomeName: 'Primary Paycheck',
		renameMerchant: null,
		requireReview: true,
		isActive: true,
		createdAt: '2026-11-02T00:00:00Z',
		updatedAt: '2026-11-02T00:00:00Z'
	}
];

describe('CategorizationRulesSection', () => {
	beforeEach(() => {
		vi.clearAllMocks();
	});

	it('renders rules, supports reordering, applying now, creating, and deleting', async () => {
		mockApi.fetchCategorizationRules.mockResolvedValue(sampleRules);
		mockApi.reorderCategorizationRules.mockResolvedValue([sampleRules[1], sampleRules[0]]);
		mockApi.applyCategorizationRule.mockResolvedValue(3);
		mockApi.deleteCategorizationRule.mockResolvedValue(undefined);
		mockApi.createCategorizationRule.mockResolvedValue({
			rule: { ...sampleRules[0], id: 'rule-3', merchantPattern: 'Whole Foods' },
			appliedCount: 2
		});

		const { getAllByTestId, getByText, getByLabelText, getByTestId } = render(
			CategorizationRulesSection,
			{
				props: { categories, accounts, incomes: [] }
			}
		);

		await waitFor(() => expect(getAllByTestId('rule-row')).toHaveLength(2));
		expect(getByText('Groceries')).toBeTruthy();
		expect(getByText('Income: Primary Paycheck')).toBeTruthy();

		// Reorder rule 2 up
		await fireEvent.click(getByLabelText('Move rule 2 up'));
		await waitFor(() =>
			expect(mockApi.reorderCategorizationRules).toHaveBeenCalledWith(['rule-2', 'rule-1'])
		);

		// Apply rule now
		await fireEvent.click(getAllByTestId('rule-row')[0].querySelector('button:nth-of-type(4)')!);
		await waitFor(() => expect(mockApi.applyCategorizationRule).toHaveBeenCalledWith('rule-2'));
		expect(getByText(/applied to 3 uncategorized transactions/)).toBeTruthy();

		// Open New Rule modal & create
		await fireEvent.click(getByTestId('new-rule-btn'));
		await fireEvent.input(getByLabelText('Merchant Pattern'), {
			target: { value: 'Whole Foods' }
		});
		await fireEvent.click(getByTestId('rule-modal-submit'));
		await waitFor(() => expect(mockApi.createCategorizationRule).toHaveBeenCalledTimes(1));

		// Delete rule
		await fireEvent.click(getAllByTestId('rule-row')[0].querySelector('button:nth-of-type(6)')!);
		await waitFor(() => expect(mockApi.deleteCategorizationRule).toHaveBeenCalled());
	});
});
