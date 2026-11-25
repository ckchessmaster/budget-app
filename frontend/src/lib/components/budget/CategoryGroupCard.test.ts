import { fireEvent, render } from '@testing-library/svelte';
import { describe, expect, it, vi } from 'vitest';
import CategoryGroupCard from './CategoryGroupCard.svelte';
import { GROUP_COLOR_PALETTE } from '$lib/utils/budgetColors';

describe('CategoryGroupCard', () => {
	it('renders Edit category button, fires onEditGroup, and triggers + Add Item', async () => {
		const onEditGroup = vi.fn();
		const onAddCategoryInGroup = vi.fn();

		const { getByTestId, getByRole } = render(CategoryGroupCard, {
			props: {
				groupName: 'Essentials',
				color: GROUP_COLOR_PALETTE[0],
				categories: [],
				displayMode: 'remaining',
				onToggleDisplayMode: vi.fn(),
				onCoverClick: vi.fn(),
				onEditClick: vi.fn(),
				onEditGroup,
				onUpdatePlanned: vi.fn(),
				onAddCategoryInGroup
			}
		});

		await fireEvent.click(getByTestId('edit-group-btn'));
		expect(onEditGroup).toHaveBeenCalledWith('Essentials');

		await fireEvent.click(getByRole('button', { name: '+ Add Item' }));
		expect(onAddCategoryInGroup).toHaveBeenCalledWith('Essentials');
	});

	it('allows dropping a category item into an empty category', async () => {
		const onMoveCategoryToGroup = vi.fn().mockResolvedValue(undefined);

		const { getByTestId } = render(CategoryGroupCard, {
			props: {
				groupName: 'Vacation',
				color: GROUP_COLOR_PALETTE[1],
				categories: [],
				displayMode: 'remaining',
				onToggleDisplayMode: vi.fn(),
				onCoverClick: vi.fn(),
				onEditClick: vi.fn(),
				onUpdatePlanned: vi.fn(),
				onAddCategoryInGroup: vi.fn(),
				onMoveCategoryToGroup
			}
		});

		const dropzone = getByTestId('empty-group-dropzone');
		await fireEvent.drop(dropzone, {
			dataTransfer: {
				getData: (type: string) =>
					type === 'application/x-budget-category-id' ? 'cat-flights' : ''
			}
		});

		expect(onMoveCategoryToGroup).toHaveBeenCalledWith('cat-flights', 'Vacation');
	});
});
