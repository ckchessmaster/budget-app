import { createRawSnippet } from 'svelte';
import { fireEvent, render } from '@testing-library/svelte';
import { describe, expect, it, vi } from 'vitest';
import TransactionsDrawer from './TransactionsDrawer.svelte';

const childSnippet = createRawSnippet(() => ({
	render: () => '<p data-testid="drawer-child">Drawer content</p>'
}));

describe('TransactionsDrawer', () => {
	it('does not render when closed', () => {
		const { queryByTestId } = render(TransactionsDrawer, {
			props: {
				isOpen: false,
				onClose: vi.fn(),
				children: childSnippet
			}
		});

		expect(queryByTestId('mobile-transactions-drawer')).toBeNull();
	});

	it('renders dialog with title, badge count, and children when open', () => {
		const { getByTestId, getByText } = render(TransactionsDrawer, {
			props: {
				isOpen: true,
				title: 'Transactions',
				badgeCount: 5,
				onClose: vi.fn(),
				children: childSnippet
			}
		});

		const drawer = getByTestId('mobile-transactions-drawer');
		expect(drawer.getAttribute('role')).toBe('dialog');
		expect(drawer.getAttribute('aria-modal')).toBe('true');
		expect(drawer.className).toContain('lg:hidden');
		expect(getByText('Transactions')).toBeTruthy();
		expect(getByText('5')).toBeTruthy();
		expect(getByTestId('drawer-child').textContent).toBe('Drawer content');
	});

	it('invokes onClose on close button click, backdrop click, and Escape key', async () => {
		const onClose = vi.fn();

		const { getByTestId } = render(TransactionsDrawer, {
			props: {
				isOpen: true,
				onClose,
				children: childSnippet
			}
		});

		await fireEvent.click(getByTestId('close-drawer-btn'));
		expect(onClose).toHaveBeenCalledTimes(1);

		await fireEvent.click(getByTestId('drawer-backdrop'));
		expect(onClose).toHaveBeenCalledTimes(2);

		await fireEvent.keyDown(window, { key: 'Escape' });
		expect(onClose).toHaveBeenCalledTimes(3);
	});
});
