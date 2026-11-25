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

	it('supports group drag-and-drop with anti-flicker dragleave and ignores item/tx drops', async () => {
		const onReorderGroup = vi.fn().mockResolvedValue(undefined);

		const { getByTestId, queryByTestId } = render(CategoryGroupCard, {
			props: {
				groupName: 'Essentials',
				color: GROUP_COLOR_PALETTE[0],
				categories: [
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
						originalPlanned: 500,
						effectiveTarget: 500,
						spentCleared: 0,
						spentPending: 0,
						remaining: 500
					}
				],
				displayMode: 'remaining',
				onToggleDisplayMode: vi.fn(),
				onCoverClick: vi.fn(),
				onEditClick: vi.fn(),
				onUpdatePlanned: vi.fn(),
				onAddCategoryInGroup: vi.fn(),
				onReorderGroup
			}
		});

		const card = getByTestId('category-group-card');
		vi.spyOn(card, 'getBoundingClientRect').mockReturnValue({
			top: 0,
			height: 200,
			bottom: 200,
			left: 0,
			right: 400,
			width: 400,
			x: 0,
			y: 0,
			toJSON: () => ({})
		});
		const handle = getByTestId('group-drag-handle');
		expect(handle).toBeTruthy();

		const dragOverEvent = new Event('dragover', { bubbles: true, cancelable: true });
		Object.defineProperty(dragOverEvent, 'clientY', { value: 20 });
		Object.defineProperty(dragOverEvent, 'dataTransfer', {
			value: { types: ['application/x-budget-group-name'], dropEffect: 'none' }
		});
		await fireEvent(card, dragOverEvent);
		expect(getByTestId('group-drop-indicator')).toBeTruthy();
		expect(card.className).not.toContain('ring-2');

		// Moving onto a child inside the card must not clear the indicator
		const childRow = getByTestId('category-row');
		const childDragLeave = new Event('dragleave', { bubbles: true });
		Object.defineProperty(childDragLeave, 'relatedTarget', { value: childRow });
		await fireEvent(card, childDragLeave);
		expect(getByTestId('group-drop-indicator')).toBeTruthy();

		await fireEvent.drop(card, {
			dataTransfer: {
				getData: (type: string) =>
					type === 'application/x-budget-group-name' ? 'Lifestyle' : ''
			}
		});
		expect(onReorderGroup).toHaveBeenCalledWith('Lifestyle', 'Essentials', 'before');
		expect(queryByTestId('group-drop-indicator')).toBeNull();

		onReorderGroup.mockClear();
		await fireEvent.drop(card, {
			dataTransfer: {
				getData: (type: string) =>
					type === 'application/x-budget-category-id' ? 'cat-other' : ''
			}
		});
		expect(onReorderGroup).not.toHaveBeenCalled();
	});

	it('renders organize mode move and collapse controls with boundary disabled states', async () => {
		const onMoveGroupStep = vi.fn().mockResolvedValue(undefined);
		const onToggleCollapse = vi.fn();

		const { getByTestId, queryByTestId, rerender } = render(CategoryGroupCard, {
			props: {
				groupName: 'Essentials',
				color: GROUP_COLOR_PALETTE[0],
				categories: [
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
						originalPlanned: 500,
						effectiveTarget: 500,
						spentCleared: 0,
						spentPending: 0,
						remaining: 500
					}
				],
				displayMode: 'remaining',
				organizeMode: true,
				isFirstGroup: true,
				isLastGroup: false,
				isCollapsed: false,
				onToggleDisplayMode: vi.fn(),
				onCoverClick: vi.fn(),
				onEditClick: vi.fn(),
				onUpdatePlanned: vi.fn(),
				onAddCategoryInGroup: vi.fn(),
				onMoveGroupStep,
				onToggleCollapse
			}
		});

		expect((getByTestId('move-group-up-btn') as HTMLButtonElement).disabled).toBe(true);
		const downBtn = getByTestId('move-group-down-btn') as HTMLButtonElement;
		expect(downBtn.disabled).toBe(false);

		await fireEvent.click(downBtn);
		expect(onMoveGroupStep).toHaveBeenCalledWith('Essentials', 'down');

		await fireEvent.click(getByTestId('toggle-group-collapse-btn'));
		expect(onToggleCollapse).toHaveBeenCalledWith('Essentials');

		expect(queryByTestId('category-row')).toBeTruthy();
		await rerender({
			groupName: 'Essentials',
			color: GROUP_COLOR_PALETTE[0],
			categories: [
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
					originalPlanned: 500,
					effectiveTarget: 500,
					spentCleared: 0,
					spentPending: 0,
					remaining: 500
				}
			],
			displayMode: 'remaining',
			organizeMode: true,
			isFirstGroup: false,
			isLastGroup: true,
			isCollapsed: true,
			onToggleDisplayMode: vi.fn(),
			onCoverClick: vi.fn(),
			onEditClick: vi.fn(),
			onUpdatePlanned: vi.fn(),
			onAddCategoryInGroup: vi.fn(),
			onMoveGroupStep,
			onToggleCollapse
		});

		expect(queryByTestId('category-row')).toBeNull();
		expect((getByTestId('move-group-down-btn') as HTMLButtonElement).disabled).toBe(true);
	});
});
