import { render, fireEvent, waitFor } from '@testing-library/svelte';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import CoverDrawer from './CoverDrawer.svelte';

describe('CoverDrawer', () => {
	beforeEach(() => {
		vi.restoreAllMocks();
	});

	it('submits POST /api/v1/budget/reallocate with donor category and exact $35.00 deficit', async () => {
		const fetchMock = vi.fn().mockResolvedValue({
			ok: true,
			json: async () => ({ status: 'reallocated' })
		});
		vi.stubGlobal('fetch', fetchMock);

		const onClose = vi.fn();
		const onSuccess = vi.fn();

		const { getByRole, getByLabelText } = render(CoverDrawer, {
			props: {
				isOpen: true,
				yearMonth: '2026-11-01',
				overspentCategory: {
					id: 'cat-overspent',
					name: 'Groceries',
					remaining: -35
				},
				availableDonors: [
					{ id: 'cat-empty', name: 'Rent', available: 0 },
					{ id: 'cat-donor', name: 'Dining', available: 120 }
				],
				onClose,
				onSuccess
			}
		});

		const donorTrigger = getByLabelText('Move Money From');
		await fireEvent.click(donorTrigger);
		const donorOptions = document.querySelectorAll('[data-testid="cover-donor-option"]');
		// Only donor with available > 0 should be selectable
		expect(donorOptions.length).toBe(1);
		expect(donorOptions[0].textContent).toContain('Dining');
		await fireEvent.click(donorTrigger);

		const amountInput = getByLabelText('Amount ($)') as HTMLInputElement;
		expect(amountInput.value).toBe('35.00');

		const submitBtn = getByRole('button', { name: 'Cover Overspend' });
		await fireEvent.click(submitBtn);

		await waitFor(() => {
			expect(fetchMock).toHaveBeenCalledTimes(1);
		});

		const [url, init] = fetchMock.mock.calls[0];
		expect(url).toContain('/api/v1/budget/reallocate');
		expect(init.method).toBe('POST');
		const body = JSON.parse(init.body);
		expect(body).toMatchObject({
			from_category_id: 'cat-donor',
			to_category_id: 'cat-overspent',
			amount: 35.0
		});
		expect(onSuccess).toHaveBeenCalledTimes(1);
	});
});
