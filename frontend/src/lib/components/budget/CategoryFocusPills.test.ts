import { fireEvent, render } from '@testing-library/svelte';
import { describe, expect, it, vi } from 'vitest';
import CategoryFocusPills, { FOCUS_ALL, FOCUS_INCOME } from './CategoryFocusPills.svelte';

describe('CategoryFocusPills', () => {
	it('renders All Categories, Income, and deduplicated group pills in order', () => {
		const { getAllByTestId } = render(CategoryFocusPills, {
			props: {
				groups: ['Essentials', 'Lifestyle', 'Essentials', 'Sinking Funds', 'Debt Paydown'],
				selected: FOCUS_ALL,
				onSelect: vi.fn()
			}
		});

		const pills = getAllByTestId('focus-pill');
		expect(pills.map((p) => p.textContent?.trim())).toEqual([
			'All Categories',
			'Income',
			'Essentials',
			'Lifestyle',
			'Sinking Funds',
			'Debt Paydown'
		]);
		expect(pills[0].getAttribute('aria-pressed')).toBe('true');
		expect(pills[1].getAttribute('aria-pressed')).toBe('false');
		expect(pills[2].getAttribute('aria-pressed')).toBe('false');
	});

	it('marks only the selected pill as pressed and fires onSelect when clicked', async () => {
		const onSelect = vi.fn();
		const { getAllByTestId } = render(CategoryFocusPills, {
			props: {
				groups: ['Essentials', 'Lifestyle'],
				selected: 'Essentials',
				onSelect
			}
		});

		const pills = getAllByTestId('focus-pill');
		expect(pills[0].getAttribute('aria-pressed')).toBe('false');
		expect(pills[1].getAttribute('aria-pressed')).toBe('false');
		expect(pills[2].getAttribute('aria-pressed')).toBe('true');

		await fireEvent.click(pills[1]);
		expect(onSelect).toHaveBeenCalledWith(FOCUS_INCOME);

		await fireEvent.click(pills[3]);
		expect(onSelect).toHaveBeenCalledWith('Lifestyle');

		await fireEvent.click(pills[0]);
		expect(onSelect).toHaveBeenCalledWith(FOCUS_ALL);
	});
});
