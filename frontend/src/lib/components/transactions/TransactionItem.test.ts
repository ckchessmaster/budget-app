import { render } from '@testing-library/svelte';
import { describe, expect, it } from 'vitest';
import TransactionItem from './TransactionItem.svelte';

describe('TransactionItem', () => {
	it('renders clock icon and muted italic styling for PENDING transactions', () => {
		const { getByTestId } = render(TransactionItem, {
			props: {
				transaction: {
					id: 'tx-1',
					accountId: 'acct-1',
					origin: 'BANK_FEED',
					status: 'PENDING',
					amount: -24.5,
					merchantRaw: 'STARBUCKS #1234',
					merchantClean: 'Starbucks',
					categoryId: null,
					transactionDate: '2026-11-16',
					userNotes: null,
					isCleared: false
				}
			}
		});

		const row = getByTestId('transaction-item');
		expect(row.className).toContain('italic');
		expect(row.className).toContain('text-slate-400');
		expect(getByTestId('badge-pending')).toBeTruthy();
	});

	it('renders cloud upload icon for MANUAL_OFFLINE QUEUED transactions', () => {
		const { getByTestId } = render(TransactionItem, {
			props: {
				transaction: {
					id: 'local-1',
					accountId: 'acct-1',
					origin: 'MANUAL_OFFLINE',
					status: 'POSTED',
					amount: -52.0,
					merchantRaw: 'Local Bakery',
					merchantClean: null,
					categoryId: 'cat-1',
					transactionDate: '2026-11-16',
					userNotes: null,
					isCleared: true,
					syncStatus: 'QUEUED'
				}
			}
		});

		expect(getByTestId('badge-queued-offline')).toBeTruthy();
	});

	it('renders amber settledDelta badge when settled amount exceeds authorization', () => {
		const { getByTestId } = render(TransactionItem, {
			props: {
				transaction: {
					id: 'tx-3',
					accountId: 'acct-1',
					origin: 'BANK_FEED',
					status: 'POSTED',
					amount: -48.0,
					merchantRaw: 'Bistro',
					merchantClean: 'Bistro',
					categoryId: 'cat-1',
					transactionDate: '2026-11-15',
					userNotes: null,
					isCleared: true,
					settledDelta: 8.0
				}
			}
		});

		const delta = getByTestId('badge-settled-delta');
		expect(delta.textContent).toContain('+$8.00');
		expect(delta.className).toContain('text-amber-300');
	});
});
