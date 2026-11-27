import { fireEvent, render } from '@testing-library/svelte';
import { describe, expect, it, vi } from 'vitest';
import CategoryModal from './CategoryModal.svelte';

describe('CategoryModal', () => {
	it('renders Monthly Due Day only once for Monthly Expense and closes on Escape without saving', async () => {
		const fetchMock = vi.fn();
		vi.stubGlobal('fetch', fetchMock);
		const onClose = vi.fn();
		const onSuccess = vi.fn();

		const { getAllByLabelText, getByLabelText } = render(CategoryModal, {
			props: {
				isOpen: true,
				editingCategory: null,
				allCategories: [
					{
						id: 'cat-1',
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
						spentCleared: 0,
						spentPending: 0,
						remaining: 400
					}
				],
				availableGroups: ['Essentials'],
				defaultGroupName: 'Essentials',
				onClose,
				onSuccess
			}
		});

		expect(getAllByLabelText('Monthly Due Day (optional)')).toHaveLength(1);

		await fireEvent.input(getByLabelText('Item Name'), { target: { value: 'Water' } });
		await fireEvent.keyDown(window, { key: 'Escape' });

		expect(onClose).toHaveBeenCalledTimes(1);
		expect(onSuccess).not.toHaveBeenCalled();
		expect(fetchMock).not.toHaveBeenCalled();
	});
});
