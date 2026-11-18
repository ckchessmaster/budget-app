import { render, screen } from '@testing-library/svelte';
import { describe, expect, it } from 'vitest';
import TransactionItem from './TransactionItem.svelte';
import type { TransactionItemModel } from '$lib/types/transaction';

describe('TransactionItem', () => {
	it('renders pending clock badge with italicized muted text', () => {
		const tx: TransactionItemModel = {
			id: 'tx-1',
			accountId: 'acct-1',
			origin: 'BANK_FEED',
			status: 'PENDING',
			amount: -24.5,
			merchantRaw: 'Corner Cafe',
			categoryId: 'cat-1',
			transactionDate: '2026-11-18',
			isCleared: false
		};

		render(TransactionItem, { props: { transaction: tx, categoryName: 'Dining' } });
		const badge = screen.getByTestId('badge-pending');
		expect(badge).toBeTruthy();
		expect(screen.getByText('Corner Cafe').className).toContain('italic');
	});

	it('renders cloud upload badge for offline queued manual transactions', () => {
		const tx: TransactionItemModel = {
			id: 'local-1',
			accountId: 'acct-1',
			origin: 'MANUAL_OFFLINE',
			status: 'PENDING',
			syncStatus: 'QUEUED',
			amount: -18.0,
			merchantRaw: 'Farmers Market',
			categoryId: 'cat-2',
			transactionDate: '2026-11-18',
			isCleared: false
		};

		render(TransactionItem, { props: { transaction: tx, categoryName: 'Groceries' } });
		expect(screen.getByTestId('badge-offline-queued')).toBeTruthy();
	});

	it('renders amber settledDelta indicator when settled amount differs', () => {
		const tx: TransactionItemModel = {
			id: 'tx-3',
			accountId: 'acct-1',
			origin: 'BANK_FEED',
			status: 'POSTED',
			amount: -65.0,
			merchantRaw: 'Restaurant',
			categoryId: 'cat-1',
			transactionDate: '2026-11-18',
			isCleared: true,
			settledDelta: 10.0
		};

		render(TransactionItem, { props: { transaction: tx } });
		const deltaBadge = screen.getByTestId('badge-settled-delta');
		expect(deltaBadge).toBeTruthy();
		expect(deltaBadge.className).toContain('amber');
		expect(deltaBadge.textContent).toContain('+$10.00');
	});
});
