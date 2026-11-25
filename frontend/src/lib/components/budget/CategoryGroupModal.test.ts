import { fireEvent, render, waitFor } from '@testing-library/svelte';
import { beforeAll, describe, expect, it, vi } from 'vitest';
import CategoryGroupModal from './CategoryGroupModal.svelte';

describe('CategoryGroupModal', () => {
	beforeAll(() => {
		HTMLDialogElement.prototype.showModal = function () {
			this.setAttribute('open', '');
		};
		HTMLDialogElement.prototype.close = function () {
			this.removeAttribute('open');
			this.dispatchEvent(new Event('close'));
		};
	});

	it('creates a category with name and selected color, and prevents duplicates', async () => {
		const onSave = vi.fn().mockResolvedValue(undefined);
		const onClose = vi.fn();

		const { getByLabelText, getByRole } = render(CategoryGroupModal, {
			props: {
				isOpen: true,
				editingGroupName: null,
				initialColorIndex: 0,
				existingGroupNames: ['Essentials'],
				onClose,
				onSave
			}
		});

		await fireEvent.input(getByLabelText('Category Name'), { target: { value: 'essentials' } });
		await fireEvent.click(getByRole('button', { name: 'Create Category' }));

		expect(getByRole('alert').textContent).toContain('already exists');
		expect(onSave).not.toHaveBeenCalled();

		await fireEvent.input(getByLabelText('Category Name'), { target: { value: 'Travel' } });
		await fireEvent.click(getByRole('button', { name: 'Emerald' }));
		await fireEvent.click(getByRole('button', { name: 'Create Category' }));

		await waitFor(() => {
			expect(onSave).toHaveBeenCalledWith({
				oldName: null,
				name: 'Travel',
				colorIndex: 2
			});
			expect(onClose).toHaveBeenCalledTimes(1);
		});
	});

	it('supports editing name and color', async () => {
		const onSave = vi.fn().mockResolvedValue(undefined);
		const onClose = vi.fn();

		const { getByLabelText, getByRole } = render(CategoryGroupModal, {
			props: {
				isOpen: true,
				editingGroupName: 'Lifestyle',
				initialColorIndex: 1,
				existingGroupNames: ['Essentials', 'Lifestyle'],
				itemCount: 2,
				onClose,
				onSave
			}
		});

		await fireEvent.input(getByLabelText('Category Name'), { target: { value: 'Fun Money' } });
		await fireEvent.click(getByRole('button', { name: 'Save Category' }));

		await waitFor(() => {
			expect(onSave).toHaveBeenCalledWith({
				oldName: 'Lifestyle',
				name: 'Fun Money',
				colorIndex: 1
			});
			expect(onClose).toHaveBeenCalledTimes(1);
		});
	});

	it('requires confirmation before deleting a category', async () => {
		const onSave = vi.fn().mockResolvedValue(undefined);
		const onDelete = vi.fn().mockResolvedValue(undefined);
		const onClose = vi.fn();

		const { getByRole, getByText } = render(CategoryGroupModal, {
			props: {
				isOpen: true,
				editingGroupName: 'Lifestyle',
				initialColorIndex: 1,
				existingGroupNames: ['Essentials', 'Lifestyle'],
				itemCount: 2,
				onClose,
				onSave,
				onDelete
			}
		});

		await fireEvent.click(getByRole('button', { name: 'Delete' }));
		expect(getByText(/and its 2 items/)).toBeTruthy();
		expect(onDelete).not.toHaveBeenCalled();

		await fireEvent.click(getByRole('button', { name: 'Confirm Delete' }));
		await waitFor(() => {
			expect(onDelete).toHaveBeenCalledWith('Lifestyle');
			expect(onClose).toHaveBeenCalledTimes(1);
		});
	});
});
