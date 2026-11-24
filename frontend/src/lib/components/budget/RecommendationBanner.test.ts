import { render, fireEvent, waitFor } from '@testing-library/svelte';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import RecommendationBanner from './RecommendationBanner.svelte';
import type { SpendRecommendation } from '$lib/types/budget';

const diningRec: SpendRecommendation = {
	categoryId: 'cat-dining',
	categoryName: 'Dining',
	currentPlanned: 500,
	trailingMedianSpend: 420,
	percentageDeviation: 0.16,
	shouldPrompt: true
};

const groceriesRec: SpendRecommendation = {
	categoryId: 'cat-groceries',
	categoryName: 'Groceries',
	currentPlanned: 500,
	trailingMedianSpend: 490,
	percentageDeviation: 0.02,
	shouldPrompt: false
};

describe('RecommendationBanner', () => {
	beforeEach(() => {
		vi.restoreAllMocks();
	});

	it('renders nothing when recommendations is empty or all items have shouldPrompt=false', () => {
		const emptyRender = render(RecommendationBanner, {
			props: {
				recommendations: [],
				yearMonth: '2026-11'
			}
		});
		expect(emptyRender.queryByTestId('recommendation-banner')).toBeNull();
		emptyRender.unmount();

		const noPromptRender = render(RecommendationBanner, {
			props: {
				recommendations: [groceriesRec],
				yearMonth: '2026-11'
			}
		});
		expect(noPromptRender.queryByTestId('recommendation-banner')).toBeNull();
	});

	it('renders exact inline prompt copy for shouldPrompt=true and skips shouldPrompt=false items', () => {
		const { getByTestId, getByText, queryByText, getAllByTestId } = render(RecommendationBanner, {
			props: {
				recommendations: [diningRec, groceriesRec],
				yearMonth: '2026-11'
			}
		});

		expect(getByTestId('recommendation-banner')).toBeTruthy();
		expect(getAllByTestId('recommendation-item')).toHaveLength(1);
		expect(
			getByText(
				'You averaged $420 on Dining over the last 3 months. Adjust target from $500 to $420?'
			)
		).toBeTruthy();
		expect(getByText('16% below target')).toBeTruthy();
		expect(queryByText(/Groceries/)).toBeNull();
	});

	it('submits PUT /api/v1/budget/categories/{id} and calls onApplied when Apply Recommended Target is clicked', async () => {
		const fetchMock = vi.fn().mockResolvedValue({
			ok: true,
			json: async () => ({ status: 'updated' })
		});
		vi.stubGlobal('fetch', fetchMock);

		const onApplied = vi.fn();
		const { getByRole } = render(RecommendationBanner, {
			props: {
				recommendations: [diningRec],
				yearMonth: '2026-11',
				onApplied
			}
		});

		await fireEvent.click(getByRole('button', { name: 'Apply Recommended Target' }));

		await waitFor(() => {
			expect(fetchMock).toHaveBeenCalledTimes(1);
		});

		const [url, init] = fetchMock.mock.calls[0];
		expect(url).toContain('/api/v1/budget/categories/cat-dining');
		expect(init.method).toBe('PUT');
		expect(JSON.parse(init.body)).toEqual({
			year_month: '2026-11',
			original_planned: 420
		});
		expect(onApplied).toHaveBeenCalledTimes(1);
		expect(onApplied).toHaveBeenCalledWith(diningRec);
	});

	it('displays inline error alert when API request fails', async () => {
		const fetchMock = vi.fn().mockResolvedValue({
			ok: false,
			status: 500,
			json: async () => ({ error: 'failed_to_update_monthly_planned' })
		});
		vi.stubGlobal('fetch', fetchMock);

		const onApplied = vi.fn();
		const { getByRole } = render(RecommendationBanner, {
			props: {
				recommendations: [diningRec],
				yearMonth: '2026-11',
				onApplied
			}
		});

		await fireEvent.click(getByRole('button', { name: 'Apply Recommended Target' }));

		await waitFor(() => {
			expect(getByRole('alert').textContent).toContain('failed_to_update_monthly_planned');
		});
		expect(onApplied).not.toHaveBeenCalled();
	});

	it('dismisses a recommendation and invokes onDismiss', async () => {
		const onDismiss = vi.fn();
		const { getByRole, queryByTestId } = render(RecommendationBanner, {
			props: {
				recommendations: [diningRec],
				yearMonth: '2026-11',
				onDismiss
			}
		});

		await fireEvent.click(
			getByRole('button', { name: 'Dismiss recommendation for Dining' })
		);

		expect(onDismiss).toHaveBeenCalledTimes(1);
		expect(onDismiss).toHaveBeenCalledWith('cat-dining');
		expect(queryByTestId('recommendation-banner')).toBeNull();
	});

	it('collapses recommendations beyond 2 behind Show N more and expands on click', async () => {
		const extraRecs: SpendRecommendation[] = [
			diningRec,
			{
				categoryId: 'cat-travel',
				categoryName: 'Travel',
				currentPlanned: 300,
				trailingMedianSpend: 400,
				percentageDeviation: 0.3333,
				shouldPrompt: true
			},
			{
				categoryId: 'cat-coffee',
				categoryName: 'Coffee',
				currentPlanned: 100,
				trailingMedianSpend: 60,
				percentageDeviation: 0.4,
				shouldPrompt: true
			}
		];

		const { getAllByTestId, getByRole } = render(RecommendationBanner, {
			props: {
				recommendations: extraRecs,
				yearMonth: '2026-11'
			}
		});

		expect(getAllByTestId('recommendation-item')).toHaveLength(2);
		await fireEvent.click(getByRole('button', { name: 'Show 1 more' }));
		expect(getAllByTestId('recommendation-item')).toHaveLength(3);
		expect(getByRole('button', { name: 'Show fewer' })).toBeTruthy();
	});
});
