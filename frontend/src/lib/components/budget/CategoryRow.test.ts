import { render, fireEvent } from '@testing-library/svelte';
import { describe, expect, it, vi } from 'vitest';
import CategoryRow from './CategoryRow.svelte';

describe('CategoryRow', () => {
	it('renders red overspent badge (-$20.00), inline pending badge, and fires onCoverClick without expanding drawer', async () => {
		const onCoverClick = vi.fn();
		const { getByTestId, getByText, queryByTestId } = render(CategoryRow, {
			props: {
				category: {
					id: 'cat-123',
					name: 'Groceries',
					groupName: 'Essentials',
					originalPlanned: 500,
					effectiveTarget: 500,
					spentCleared: 480,
					spentPending: 40,
					remaining: -20
				},
				onCoverClick
			}
		});

		expect(getByText('Groceries')).toBeTruthy();
		const pendingBadge = getByTestId('pending-badge');
		expect(pendingBadge.textContent?.trim()).toBe('+$40.00 pend');

		const badge = getByTestId('overspent-badge');
		expect(badge.textContent).toContain('-$20.00');
		expect(badge.className).toContain('text-status-danger');

		await fireEvent.click(badge);
		expect(onCoverClick).toHaveBeenCalledTimes(1);
		expect(onCoverClick).toHaveBeenCalledWith('cat-123');
		expect(queryByTestId('inline-activity-drawer')).toBeNull();
	});

	it('renders slim variable progress bar with striped pending layer and original planned tick mark', () => {
		const { getByTestId } = render(CategoryRow, {
			props: {
				category: {
					id: 'cat-456',
					name: 'Dining Out',
					groupName: 'Lifestyle',
					originalPlanned: 300,
					effectiveTarget: 250,
					spentCleared: 100,
					spentPending: 25,
					remaining: 125
				},
				onCoverClick: vi.fn()
			}
		});

		expect(getByTestId('variable-progress-bar')).toBeTruthy();
		expect(getByTestId('bar-cleared')).toBeTruthy();
		const pendingBar = getByTestId('bar-pending');
		expect(pendingBar).toBeTruthy();
		expect(pendingBar.className).toContain('bg-pending-stripe');
		expect(getByTestId('bar-original-tick')).toBeTruthy();
		expect(getByTestId('pending-badge').textContent?.trim()).toBe('+$25.00 pend');
	});

	it('renders Paid or Upcoming badge instead of progress bar for fixed categories', () => {
		const { getByTestId, queryByTestId, unmount } = render(CategoryRow, {
			props: {
				category: {
					id: 'cat-rent',
					name: 'Rent/Mortgage',
					groupName: 'Essentials',
					originalPlanned: 2000,
					effectiveTarget: 2000,
					spentCleared: 2000,
					spentPending: 0,
					remaining: 0
				},
				onCoverClick: vi.fn()
			}
		});

		expect(getByTestId('fixed-status-badge').textContent?.trim()).toBe('Paid');
		expect(queryByTestId('variable-progress-bar')).toBeNull();
		unmount();

		const upcoming = render(CategoryRow, {
			props: {
				category: {
					id: 'cat-util',
					name: 'Utilities',
					groupName: 'Essentials',
					originalPlanned: 180,
					effectiveTarget: 180,
					spentCleared: 0,
					spentPending: 0,
					remaining: 180
				},
				onCoverClick: vi.fn()
			}
		});

		expect(upcoming.getByTestId('fixed-status-badge').textContent?.trim()).toBe('Upcoming');
		expect(upcoming.queryByTestId('variable-progress-bar')).toBeNull();
	});

	it('expands and collapses inline activity drawer, hides Txns button, and fires onSelectTransaction cleanly', async () => {
		const onCoverClick = vi.fn();
		const onEditClick = vi.fn();
		const onSelectTransaction = vi.fn();
		const { getByTestId, queryByTestId, queryByText, getByText } = render(CategoryRow, {
			props: {
				category: {
					id: 'cat-789',
					name: 'Groceries',
					groupName: 'Essentials',
					originalPlanned: 400,
					effectiveTarget: 400,
					spentCleared: 72.5,
					spentPending: 0,
					remaining: 327.5
				},
				transactions: [
					{
						id: 'tx-1:split-1',
						transactionId: 'tx-1',
						date: '2026-11-12',
						merchant: "Trader Joe's",
						accountName: 'Sapphire',
						amount: -72.5,
						status: 'POSTED'
					}
				],
				onCoverClick,
				onEditClick,
				onSelectTransaction
			}
		});

		expect(queryByText('Txns')).toBeNull();
		expect(queryByTestId('inline-activity-drawer')).toBeNull();

		await fireEvent.click(getByTestId('edit-category-btn'));
		expect(onEditClick).toHaveBeenCalledTimes(1);
		expect(onEditClick).toHaveBeenCalledWith('cat-789');
		expect(queryByTestId('inline-activity-drawer')).toBeNull();

		await fireEvent.click(getByTestId('category-row'));
		expect(getByTestId('inline-activity-drawer')).toBeTruthy();
		expect(getByText(/Nov 12 · Trader Joe's/)).toBeTruthy();
		expect(getByText('(Sapphire)')).toBeTruthy();
		expect(getByText('-$72.50')).toBeTruthy();

		await fireEvent.click(getByTestId('inline-activity-btn'));
		expect(onSelectTransaction).toHaveBeenCalledTimes(1);
		expect(onSelectTransaction).toHaveBeenCalledWith('tx-1');
		// Clicking a transaction line item should not collapse the drawer
		expect(getByTestId('inline-activity-drawer')).toBeTruthy();

		await fireEvent.click(getByTestId('category-row'));
		expect(queryByTestId('inline-activity-drawer')).toBeNull();
	});

	it('shows empty state inside expanded inline activity drawer when no transactions exist', async () => {
		const { getByTestId, getByText } = render(CategoryRow, {
			props: {
				category: {
					id: 'cat-empty',
					name: 'Personal Care',
					originalPlanned: 100,
					effectiveTarget: 100,
					spentCleared: 0,
					spentPending: 0,
					remaining: 100
				},
				onCoverClick: vi.fn()
			}
		});

		await fireEvent.click(getByTestId('category-row'));
		expect(getByTestId('inline-activity-drawer')).toBeTruthy();
		expect(getByText('No tracked transactions yet.')).toBeTruthy();
	});

	it('supports inline planned amount editing without firing onEditClick or expanding drawer', async () => {
		const onUpdatePlanned = vi.fn().mockResolvedValue(undefined);
		const onEditClick = vi.fn();
		const { getByTestId, queryByTestId } = render(CategoryRow, {
			props: {
				category: {
					id: 'cat-inline',
					name: 'Groceries',
					originalPlanned: 500,
					effectiveTarget: 500,
					spentCleared: 100,
					spentPending: 0,
					remaining: 400
				},
				onCoverClick: vi.fn(),
				onEditClick,
				onUpdatePlanned
			}
		});

		const plannedBtn = getByTestId('inline-planned-btn');
		await fireEvent.click(plannedBtn);
		expect(onEditClick).not.toHaveBeenCalled();
		expect(queryByTestId('inline-activity-drawer')).toBeNull();

		const input = getByTestId('inline-planned-input') as HTMLInputElement;
		await fireEvent.input(input, { target: { value: '625.50' } });
		await fireEvent.keyDown(input, { key: 'Enter' });

		expect(onUpdatePlanned).toHaveBeenCalledTimes(1);
		expect(onUpdatePlanned).toHaveBeenCalledWith('cat-inline', 625.5);
		expect(onEditClick).not.toHaveBeenCalled();
	});

	it('displays spent amount when displayMode is spent', () => {
		const { getByTestId } = render(CategoryRow, {
			props: {
				category: {
					id: 'cat-spent',
					name: 'Dining Out',
					originalPlanned: 200,
					effectiveTarget: 200,
					spentCleared: 75,
					spentPending: 25,
					remaining: 100
				},
				displayMode: 'spent',
				onCoverClick: vi.fn()
			}
		});

		expect(getByTestId('status-badge').textContent?.trim()).toBe('$100.00');
	});

	it('distinguishes category drag-and-drop reordering from transaction drops', async () => {
		const onReorderCategory = vi.fn().mockResolvedValue(undefined);
		const onDropTransaction = vi.fn().mockResolvedValue(undefined);

		const { getByTestId } = render(CategoryRow, {
			props: {
				category: {
					id: 'cat-target',
					name: 'Utilities',
					originalPlanned: 150,
					effectiveTarget: 150,
					spentCleared: 0,
					spentPending: 0,
					remaining: 150
				},
				onCoverClick: vi.fn(),
				onReorderCategory,
				onDropTransaction
			}
		});

		expect(getByTestId('category-drag-handle')).toBeTruthy();
		const container = getByTestId('category-row-container');

		await fireEvent.drop(container, {
			dataTransfer: {
				getData: (type: string) =>
					type === 'application/x-budget-category-id' ? 'cat-source' : ''
			}
		});
		expect(onReorderCategory).toHaveBeenCalledWith('cat-source', 'cat-target', 'before');
		expect(onDropTransaction).not.toHaveBeenCalled();

		await fireEvent.drop(container, {
			dataTransfer: {
				getData: (type: string) => (type === 'application/x-budget-tx-id' ? 'tx-99' : '')
			}
		});
		expect(onDropTransaction).toHaveBeenCalledWith('tx-99', 'cat-target');
	});
});
