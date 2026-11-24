import { render, screen } from '@testing-library/svelte';
import { describe, expect, it } from 'vitest';
import TransactionItem from './TransactionItem.svelte';
import type { TransactionItemModel } from '$lib/types/transaction';

describe('TransactionItem', () => {
	it('renders pending badge with semantic warning token and italicized merchant text', () => {
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
		expect(badge.className).toContain('text-status-warn');
		expect(screen.getByText('Corner Cafe').className).toContain('italic');
	});

	it('renders New badge for uncategorized posted on-budget transactions', () => {
		const tx: TransactionItemModel = {
			id: 'tx-new',
			accountId: 'acct-1',
			origin: 'BANK_FEED',
			status: 'POSTED',
			amount: -19.99,
			merchantRaw: 'Bookstore',
			categoryId: null,
			transactionDate: '2026-11-18',
			isCleared: true
		};

		render(TransactionItem, { props: { transaction: tx } });
		const badge = screen.getByTestId('badge-new');
		expect(badge.textContent?.trim()).toBe('New');
		expect(badge.className).toContain('bg-accent-tint');
		expect(badge.className).toContain('text-accent-pop');
	});

	it('omits New badge when transaction is categorized', () => {
		const tx: TransactionItemModel = {
			id: 'tx-cat',
			accountId: 'acct-1',
			origin: 'BANK_FEED',
			status: 'POSTED',
			amount: -19.99,
			merchantRaw: 'Bookstore',
			categoryId: 'cat-1',
			transactionDate: '2026-11-18',
			isCleared: true
		};

		render(TransactionItem, { props: { transaction: tx, categoryName: 'Shopping' } });
		expect(screen.queryByTestId('badge-new')).toBeNull();
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

	it('renders settledDelta indicator with semantic warning token when settled amount differs', () => {
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
		expect(deltaBadge.className).toContain('text-status-warn');
		expect(deltaBadge.textContent).toContain('+$10.00');
	});
});
