import { fireEvent, render, waitFor } from '@testing-library/svelte';
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import type { AccountItem } from '$lib/types/transaction';

const mockCreateAccount = vi.fn();
const mockUpdateAccount = vi.fn();
const mockDeleteAccount = vi.fn();

vi.mock('$lib/api/transactions', () => ({
	createAccount: (...args: unknown[]) => mockCreateAccount(...args),
	updateAccount: (...args: unknown[]) => mockUpdateAccount(...args),
	deleteAccount: (...args: unknown[]) => mockDeleteAccount(...args)
}));

import AccountModal from './AccountModal.svelte';

const sampleCategories = [
	{ id: 'cat-debt', name: 'Credit Card Payoff', groupName: 'Debt' }
];

function makeAccount(overrides: Partial<AccountItem> = {}): AccountItem {
	return {
		id: 'acct-1',
		name: 'Wallet Cash',
		type: 'cash',
		currentBalance: 120,
		isOnBudget: true,
		cardMode: 'PAY_IN_FULL',
		aprPercentage: 0,
		externalAccountId: null,
		initialDebtBaseline: 0,
		statementDueDay: null,
		debtPayoffCategoryId: null,
		...overrides
	};
}

describe('AccountModal', () => {
	beforeAll(() => {
		HTMLDialogElement.prototype.showModal = function () {
			this.setAttribute('open', '');
		};
		HTMLDialogElement.prototype.close = function () {
			this.removeAttribute('open');
			this.dispatchEvent(new Event('close'));
		};
	});

	beforeEach(() => {
		mockCreateAccount.mockReset();
		mockUpdateAccount.mockReset();
		mockDeleteAccount.mockReset();
	});

	it('shows balance-tracked note for investment and loan types without on-budget toggle', async () => {
		const { getByLabelText, queryByLabelText, getByTestId, queryByTestId } = render(AccountModal, {
			props: {
				open: true,
				editingAccount: null,
				categories: sampleCategories,
				onClose: vi.fn(),
				onSaved: vi.fn()
			}
		});

		const typeSelect = getByLabelText('Account Type') as HTMLSelectElement;
		expect(typeSelect.value).toBe('checking');
		expect(queryByLabelText(/On-Budget Account/i)).toBeNull();
		expect(queryByTestId('account-tracking-note')).toBeNull();

		await fireEvent.change(typeSelect, { target: { value: 'investment' } });
		expect(getByTestId('account-tracking-note')).toBeTruthy();

		await fireEvent.change(typeSelect, { target: { value: 'loan' } });
		expect(getByTestId('account-tracking-note')).toBeTruthy();

		await fireEvent.change(typeSelect, { target: { value: 'savings' } });
		expect(queryByTestId('account-tracking-note')).toBeNull();
	});

	it('stores credit card Amount Owed as negative balance and supports overpaid credit toggle', async () => {
		mockCreateAccount.mockResolvedValue(makeAccount({ id: 'acct-cc', type: 'credit_card', currentBalance: -250.5 }));
		const onSaved = vi.fn();

		const { getByLabelText, getByRole } = render(AccountModal, {
			props: {
				open: true,
				editingAccount: null,
				categories: sampleCategories,
				onClose: vi.fn(),
				onSaved
			}
		});

		await fireEvent.input(getByLabelText('Account Name'), { target: { value: 'Visa' } });
		await fireEvent.change(getByLabelText('Account Type'), { target: { value: 'credit_card' } });
		await fireEvent.input(getByLabelText('Amount Owed ($)'), { target: { value: '250.50' } });

		await fireEvent.click(getByRole('button', { name: 'Create Account' }));
		await waitFor(() => expect(mockCreateAccount).toHaveBeenCalledTimes(1));
		expect(mockCreateAccount.mock.calls[0][0]).toMatchObject({
			name: 'Visa',
			type: 'credit_card',
			currentBalance: -250.5,
			isOnBudget: true
		});
		expect(onSaved).toHaveBeenCalledTimes(1);

		// Toggle positive/credit balance and verify positive sign
		await fireEvent.click(getByLabelText(/positive \/ credit balance/i));
		await fireEvent.click(getByRole('button', { name: 'Create Account' }));
		await waitFor(() => expect(mockCreateAccount).toHaveBeenCalledTimes(2));
		expect(mockCreateAccount.mock.calls[1][0]).toMatchObject({
			currentBalance: 250.5
		});
	});

	it('shows balance adjustment checkbox when editing a manual account balance', async () => {
		const acct = makeAccount({ id: 'acct-cash', currentBalance: 100 });
		mockUpdateAccount.mockResolvedValue({ ...acct, currentBalance: 145 });

		const { getByLabelText, getByRole } = render(AccountModal, {
			props: {
				open: true,
				editingAccount: acct,
				categories: sampleCategories,
				onClose: vi.fn(),
				onSaved: vi.fn()
			}
		});

		await fireEvent.input(getByLabelText('Current Balance ($)'), { target: { value: '145.00' } });

		const adjCheckbox = getByLabelText(/Record balance adjustment transaction/i) as HTMLInputElement;
		expect(adjCheckbox.checked).toBe(false);

		await fireEvent.click(adjCheckbox);
		await fireEvent.click(getByRole('button', { name: 'Save Changes' }));

		await waitFor(() => expect(mockUpdateAccount).toHaveBeenCalledTimes(1));
		expect(mockUpdateAccount).toHaveBeenCalledWith('acct-cash', expect.objectContaining({
			currentBalance: 145,
			recordBalanceAdjustment: true
		}));
	});

	it('keeps SimpleFIN account balance read-only and supports unlinking', async () => {
		const linkedAcct = makeAccount({
			id: 'acct-sf',
			name: 'Bank Checking',
			type: 'checking',
			currentBalance: 1200,
			externalAccountId: 'ext-123'
		});
		mockUpdateAccount.mockResolvedValue({ ...linkedAcct, externalAccountId: null });
		const onSaved = vi.fn();

		const { queryByLabelText, getByRole, getByLabelText } = render(AccountModal, {
			props: {
				open: true,
				editingAccount: linkedAcct,
				categories: sampleCategories,
				onClose: vi.fn(),
				onSaved
			}
		});

		expect(queryByLabelText(/Current Balance/i)).toBeNull();

		await fireEvent.input(getByLabelText('Account Name'), { target: { value: 'Renamed Checking' } });
		await fireEvent.click(getByRole('button', { name: 'Save Changes' }));

		await waitFor(() => expect(mockUpdateAccount).toHaveBeenCalledTimes(1));
		expect(mockUpdateAccount.mock.calls[0][1].currentBalance).toBeUndefined();

		await fireEvent.click(getByRole('button', { name: 'Unlink from SimpleFIN' }));
		await fireEvent.click(getByRole('button', { name: 'Confirm Unlink' }));

		await waitFor(() => expect(mockUpdateAccount).toHaveBeenCalledTimes(2));
		expect(mockUpdateAccount).toHaveBeenLastCalledWith('acct-sf', { unlinkExternal: true });
		expect(onSaved).toHaveBeenCalledTimes(2);
	});

	it('requires confirmation before deleting an account', async () => {
		const acct = makeAccount({ id: 'acct-del', name: 'Old Cash' });
		mockDeleteAccount.mockResolvedValue(undefined);
		const onSaved = vi.fn();

		const { getByRole } = render(AccountModal, {
			props: {
				open: true,
				editingAccount: acct,
				categories: sampleCategories,
				onClose: vi.fn(),
				onSaved
			}
		});

		await fireEvent.click(getByRole('button', { name: 'Delete' }));
		expect(mockDeleteAccount).not.toHaveBeenCalled();

		await fireEvent.click(getByRole('button', { name: 'Confirm Delete' }));
		await waitFor(() => expect(mockDeleteAccount).toHaveBeenCalledWith('acct-del'));
		expect(onSaved).toHaveBeenCalledTimes(1);
	});

	it('shows SimpleFIN switch callout in create mode and hides it when editing or in demo mode', async () => {
		const onOpenSimpleFIN = vi.fn();
		const connected = render(AccountModal, {
			props: {
				open: true,
				editingAccount: null,
				categories: sampleCategories,
				simplefinConnected: true,
				isDemoMode: false,
				onOpenSimpleFIN,
				onClose: vi.fn(),
				onSaved: vi.fn()
			}
		});

		const btn = connected.getByTestId('switch-to-simplefin-btn');
		expect(btn.textContent?.trim()).toBe('Fetch from SimpleFIN');
		await fireEvent.click(btn);
		expect(onOpenSimpleFIN).toHaveBeenCalledTimes(1);
		connected.unmount();

		const disconnected = render(AccountModal, {
			props: {
				open: true,
				editingAccount: null,
				categories: sampleCategories,
				simplefinConnected: false,
				isDemoMode: false,
				onOpenSimpleFIN: vi.fn(),
				onClose: vi.fn(),
				onSaved: vi.fn()
			}
		});
		expect(disconnected.getByTestId('switch-to-simplefin-btn').textContent?.trim()).toBe(
			'Connect SimpleFIN'
		);
		disconnected.unmount();

		const editing = render(AccountModal, {
			props: {
				open: true,
				editingAccount: makeAccount(),
				categories: sampleCategories,
				simplefinConnected: true,
				isDemoMode: false,
				onOpenSimpleFIN: vi.fn(),
				onClose: vi.fn(),
				onSaved: vi.fn()
			}
		});
		expect(editing.queryByTestId('account-modal-simplefin-callout')).toBeNull();
		editing.unmount();

		const demo = render(AccountModal, {
			props: {
				open: true,
				editingAccount: null,
				categories: sampleCategories,
				simplefinConnected: true,
				isDemoMode: true,
				onOpenSimpleFIN: vi.fn(),
				onClose: vi.fn(),
				onSaved: vi.fn()
			}
		});
		expect(demo.queryByTestId('account-modal-simplefin-callout')).toBeNull();
	});
});
