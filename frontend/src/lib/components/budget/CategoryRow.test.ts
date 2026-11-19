import { render, fireEvent } from '@testing-library/svelte';
import { describe, expect, it, vi } from 'vitest';
import CategoryRow from './CategoryRow.svelte';

describe('CategoryRow', () => {
	it('renders red overspent badge (-$20.00) and fires onCoverClick when clicked', async () => {
		const onCoverClick = vi.fn();
		const { getByTestId, getByText } = render(CategoryRow, {
			props: {
				category: {
					id: 'cat-123',
					name: 'Groceries',
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
		const badge = getByTestId('overspent-badge');
		expect(badge.textContent).toContain('-$20.00');
		expect(badge.className).toContain('text-rose-400');

		await fireEvent.click(badge);
		expect(onCoverClick).toHaveBeenCalledTimes(1);
		expect(onCoverClick).toHaveBeenCalledWith('cat-123');
	});

	it('renders original planned tick mark when effectiveTarget differs from originalPlanned', () => {
		const { getByTestId } = render(CategoryRow, {
			props: {
				category: {
					id: 'cat-456',
					name: 'Dining',
					originalPlanned: 300,
					effectiveTarget: 250,
					spentCleared: 100,
					spentPending: 25,
					remaining: 125
				},
				onCoverClick: vi.fn()
			}
		});

		expect(getByTestId('bar-cleared')).toBeTruthy();
		expect(getByTestId('bar-pending')).toBeTruthy();
		expect(getByTestId('bar-original-tick')).toBeTruthy();
	});

	it('fires onEditClick when clicking the row and stops propagation on overspent badge', async () => {
		const onCoverClick = vi.fn();
		const onEditClick = vi.fn();
		const { getByTestId } = render(CategoryRow, {
			props: {
				category: {
					id: 'cat-789',
					name: 'Utilities',
					originalPlanned: 100,
					effectiveTarget: 100,
					spentCleared: 120,
					spentPending: 0,
					remaining: -20
				},
				onCoverClick,
				onEditClick
			}
		});

		await fireEvent.click(getByTestId('overspent-badge'));
		expect(onCoverClick).toHaveBeenCalledTimes(1);
		expect(onEditClick).not.toHaveBeenCalled();

		await fireEvent.click(getByTestId('category-row'));
		expect(onEditClick).toHaveBeenCalledTimes(1);
		expect(onEditClick).toHaveBeenCalledWith('cat-789');
	});
});
