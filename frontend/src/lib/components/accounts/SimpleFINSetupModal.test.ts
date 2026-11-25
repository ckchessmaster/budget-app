import { fireEvent, render, waitFor } from '@testing-library/svelte';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import type { SimpleFINDiscoveredAccount, SimpleFINClaimResult, WorkflowStatus } from '$lib/api/simplefin';

const { claimMock, discoverMock, linkMock } = vi.hoisted(() => ({
	claimMock: vi.fn<
		(
			token: string,
			onProgress?: (status: WorkflowStatus<SimpleFINClaimResult>) => void
		) => Promise<SimpleFINDiscoveredAccount[]>
	>(),
	discoverMock: vi.fn<
		(onProgress?: (status: WorkflowStatus<SimpleFINClaimResult>) => void) => Promise<
			SimpleFINDiscoveredAccount[]
		>
	>(),
	linkMock: vi.fn()
}));

vi.mock('$lib/api/simplefin', () => ({
	claimSimpleFINToken: (...args: Parameters<typeof claimMock>) => claimMock(...args),
	discoverSimpleFINAccounts: (...args: Parameters<typeof discoverMock>) => discoverMock(...args),
	linkSimpleFINAccounts: (...args: unknown[]) => linkMock(...args)
}));

import SimpleFINSetupModal from './SimpleFINSetupModal.svelte';

const VALID_TOKEN = btoa('https://beta-bridge.simplefin.org/simplefin/claim/test-token-123');

describe('SimpleFINSetupModal', () => {
	const originalClipboardDescriptor = Object.getOwnPropertyDescriptor(navigator, 'clipboard');

	beforeAll(() => {
		HTMLDialogElement.prototype.showModal = function () {
			this.setAttribute('open', '');
		};
		HTMLDialogElement.prototype.close = function () {
			this.removeAttribute('open');
			this.dispatchEvent(new Event('close'));
		};
	});

	beforeEach(() => {
		claimMock.mockReset();
		discoverMock.mockReset();
		linkMock.mockReset();
	});

	afterEach(() => {
		if (originalClipboardDescriptor) {
			Object.defineProperty(navigator, 'clipboard', originalClipboardDescriptor);
		} else {
			delete (navigator as unknown as Record<string, unknown>).clipboard;
		}
	});

	it('shows an expanded walkthrough with external SimpleFIN links on first-time connect', () => {
		const { getByTestId } = render(SimpleFINSetupModal, {
			props: {
				open: true,
				isDemoMode: false,
				isConnected: false,
				onClose: vi.fn(),
				onLinked: vi.fn()
			}
		});

		const guide = getByTestId('simplefin-guide') as HTMLDetailsElement;
		expect(guide.open).toBe(true);

		const createLink = getByTestId('open-simplefin-create') as HTMLAnchorElement;
		expect(createLink.getAttribute('href')).toBe('https://beta-bridge.simplefin.org/simplefin/create');
		expect(createLink.getAttribute('target')).toBe('_blank');
		expect(createLink.getAttribute('rel')).toBe('noopener noreferrer');

		const homeLink = getByTestId('open-simplefin-home') as HTMLAnchorElement;
		expect(homeLink.getAttribute('href')).toBe('https://beta-bridge.simplefin.org/');
	});

	it('prioritizes discovering accounts and collapses new-token setup when already connected', () => {
		const { getByTestId, getByRole } = render(SimpleFINSetupModal, {
			props: {
				open: true,
				isDemoMode: false,
				isConnected: true,
				onClose: vi.fn(),
				onLinked: vi.fn()
			}
		});

		expect(getByRole('heading', { level: 1 }).textContent).toContain('Manage SimpleFIN Bridge');
		expect(getByTestId('discover-existing-simplefin')).toBeTruthy();

		const reconnectSection = getByTestId('reconnect-token-section') as HTMLDetailsElement;
		expect(reconnectSection.open).toBe(false);

		const guide = getByTestId('simplefin-guide') as HTMLDetailsElement;
		expect(guide.open).toBe(false);

		const quickLink = getByTestId('reconnect-simplefin-create') as HTMLAnchorElement;
		expect(quickLink.getAttribute('href')).toBe('https://beta-bridge.simplefin.org/simplefin/create');
	});

	it('blocks real token entry in Demo Mode and allows exiting Demo Mode', async () => {
		const onExitDemoMode = vi.fn().mockResolvedValue(undefined);
		const { getByTestId, queryByLabelText } = render(SimpleFINSetupModal, {
			props: {
				open: true,
				isDemoMode: true,
				onClose: vi.fn(),
				onLinked: vi.fn(),
				onExitDemoMode
			}
		});

		expect(getByTestId('simplefin-demo-blocked')).toBeTruthy();
		expect(queryByLabelText('SimpleFIN Setup Token')).toBeNull();

		await fireEvent.click(getByTestId('exit-demo-mode-btn'));
		expect(onExitDemoMode).toHaveBeenCalledTimes(1);
	});

	it('validates invalid pasted URLs client-side without calling the claim API', async () => {
		const { getByLabelText, getByTestId, queryByTestId } = render(SimpleFINSetupModal, {
			props: {
				open: true,
				isDemoMode: false,
				isConnected: false,
				onClose: vi.fn(),
				onLinked: vi.fn()
			}
		});

		const textarea = getByLabelText('SimpleFIN Setup Token') as HTMLTextAreaElement;
		expect(textarea.getAttribute('aria-invalid')).toBe('false');

		await fireEvent.input(textarea, {
			target: { value: 'https://beta-bridge.simplefin.org/simplefin/create' }
		});
		await fireEvent.blur(textarea);

		expect(textarea.getAttribute('aria-invalid')).toBe('true');
		expect(textarea.getAttribute('aria-describedby')).toContain('sf-token-error');
		expect(getByTestId('sf-token-error').textContent).toContain('web link');

		await fireEvent.click(getByTestId('claim-token-btn'));
		expect(claimMock).not.toHaveBeenCalled();

		await fireEvent.input(textarea, { target: { value: VALID_TOKEN } });
		expect(textarea.getAttribute('aria-invalid')).toBe('false');
		expect(queryByTestId('sf-token-error')).toBeNull();
		expect(getByTestId('token-valid-indicator')).toBeTruthy();
	});

	it('supports one-click clipboard Paste and handles denied clipboard access gracefully', async () => {
		const readText = vi.fn().mockResolvedValueOnce(VALID_TOKEN).mockRejectedValueOnce(new Error('denied'));
		Object.defineProperty(navigator, 'clipboard', {
			value: { readText },
			configurable: true
		});

		const { getByLabelText, getByTestId, getByText } = render(SimpleFINSetupModal, {
			props: {
				open: true,
				isDemoMode: false,
				isConnected: false,
				onClose: vi.fn(),
				onLinked: vi.fn()
			}
		});

		await fireEvent.click(getByTestId('paste-token-btn'));
		const textarea = getByLabelText('SimpleFIN Setup Token') as HTMLTextAreaElement;
		await waitFor(() => {
			expect(textarea.value).toBe(VALID_TOKEN);
			expect(getByTestId('token-valid-indicator')).toBeTruthy();
		});

		await fireEvent.click(getByTestId('paste-token-btn'));
		await waitFor(() => {
			expect(getByText(/Clipboard access was blocked/)).toBeTruthy();
		});
	});

	it('translates rejected single-use token errors into a friendly recovery message', async () => {
		claimMock.mockRejectedValue(new Error('simplefin rejected the credentials'));

		const { getByLabelText, getByRole, getByTestId } = render(SimpleFINSetupModal, {
			props: {
				open: true,
				isDemoMode: false,
				isConnected: false,
				onClose: vi.fn(),
				onLinked: vi.fn()
			}
		});

		const textarea = getByLabelText('SimpleFIN Setup Token');
		await fireEvent.input(textarea, { target: { value: VALID_TOKEN } });
		await fireEvent.click(getByTestId('claim-token-btn'));

		await waitFor(() => {
			expect(getByRole('alert').textContent).toContain('single-use');
		});
	});

	it('infers account types, lets users select accounts to link, and auto-triggers initial sync', async () => {
		const discovered: SimpleFINDiscoveredAccount[] = [
			{ id: 'ext-1', name: 'Chase Sapphire Reserve', currency: 'USD', balance: '-420.50' },
			{ id: 'ext-2', name: 'Primary Checking', currency: 'USD', balance: '2500.00' }
		];
		claimMock.mockImplementation(async (_token, onProgress) => {
			onProgress?.({
				workflow_id: 'wf-1',
				status: 'COMPLETED',
				result: {
					connection_id: 'ws-1',
					accounts: discovered,
					accounts_discovered: 2,
					warnings: ['Bank refresh pending for 1 institution']
				}
			});
			return discovered;
		});
		linkMock.mockResolvedValue(undefined);

		const onClose = vi.fn();
		const onLinked = vi.fn();
		const onTriggerSync = vi.fn();

		const { getByLabelText, getByTestId, getByText } = render(SimpleFINSetupModal, {
			props: {
				open: true,
				isDemoMode: false,
				isConnected: false,
				onClose,
				onLinked,
				onTriggerSync
			}
		});

		await fireEvent.input(getByLabelText('SimpleFIN Setup Token'), { target: { value: VALID_TOKEN } });
		await fireEvent.click(getByTestId('claim-token-btn'));

		await waitFor(() => {
			expect(getByText('Map Discovered Accounts')).toBeTruthy();
		});

		expect(getByText('Bank refresh pending for 1 institution')).toBeTruthy();
		const firstType = document.getElementById('type-0') as HTMLSelectElement;
		const secondType = document.getElementById('type-1') as HTMLSelectElement;
		expect(firstType.value).toBe('credit_card');
		expect(secondType.value).toBe('checking');

		// Uncheck Chase Sapphire Reserve so only Primary Checking is linked
		const firstCheckbox = getByLabelText('Include Chase Sapphire Reserve') as HTMLInputElement;
		await fireEvent.click(firstCheckbox);

		const confirmBtn = getByTestId('confirm-link-btn');
		expect(confirmBtn.textContent?.trim()).toBe('Link 1 Account');

		await fireEvent.click(confirmBtn);

		await waitFor(() => {
			expect(linkMock).toHaveBeenCalledWith([
				{
					external_account_id: 'ext-2',
					name: 'Primary Checking',
					type: 'checking',
					card_mode: 'PAY_IN_FULL',
					balance: '2500.00'
				}
			]);
			expect(onLinked).toHaveBeenCalledWith(1);
			expect(onTriggerSync).toHaveBeenCalledTimes(1);
			expect(onClose).toHaveBeenCalledTimes(1);
		});
	});

	it('renders a helpful empty state when SimpleFIN returns 0 accounts', async () => {
		claimMock.mockResolvedValue([]);

		const { getByLabelText, getByTestId } = render(SimpleFINSetupModal, {
			props: {
				open: true,
				isDemoMode: false,
				isConnected: false,
				onClose: vi.fn(),
				onLinked: vi.fn()
			}
		});

		await fireEvent.input(getByLabelText('SimpleFIN Setup Token'), { target: { value: VALID_TOKEN } });
		await fireEvent.click(getByTestId('claim-token-btn'));

		await waitFor(() => {
			expect(getByTestId('simplefin-no-accounts')).toBeTruthy();
		});
		expect(getByTestId('open-simplefin-accounts').getAttribute('href')).toBe(
			'https://beta-bridge.simplefin.org/my-account'
		);
	});

	it('separates newly discovered accounts from already-linked accounts and submits only new ones', async () => {
		discoverMock.mockResolvedValue([
			{ id: 'ext-1', name: 'Existing Checking', currency: 'USD', balance: '2891.10' },
			{ id: 'ext-2', name: 'New Amex Gold', currency: 'USD', balance: '-145.20' }
		]);
		linkMock.mockResolvedValue(undefined);

		const onLinked = vi.fn();
		const onTriggerSync = vi.fn();
		const { getByTestId, getByText, getAllByTestId, queryByLabelText } = render(SimpleFINSetupModal, {
			props: {
				open: true,
				isDemoMode: false,
				isConnected: true,
				linkedExternalIds: ['ext-1'],
				onClose: vi.fn(),
				onLinked,
				onTriggerSync
			}
		});

		await fireEvent.click(getByTestId('discover-existing-simplefin'));

		await waitFor(() => {
			expect(getByText('Map Discovered Accounts')).toBeTruthy();
		});

		expect(discoverMock).toHaveBeenCalledTimes(1);
		expect(getByText('1 of 1 new accounts selected')).toBeTruthy();
		expect(getByText('$-145.20')).toBeTruthy();
		expect(queryByLabelText('Include Existing Checking')).toBeNull();
		expect(getAllByTestId('already-linked-account')).toHaveLength(1);
		expect(getByText('Existing Checking')).toBeTruthy();

		const confirmBtn = getByTestId('confirm-link-btn');
		expect(confirmBtn.textContent?.trim()).toBe('Link 1 Account');
		await fireEvent.click(confirmBtn);

		await waitFor(() => {
			expect(linkMock).toHaveBeenCalledWith([
				{
					external_account_id: 'ext-2',
					name: 'New Amex Gold',
					type: 'credit_card',
					card_mode: 'PAY_IN_FULL',
					balance: '-145.20'
				}
			]);
			expect(onLinked).toHaveBeenCalledWith(1);
			expect(onTriggerSync).toHaveBeenCalledTimes(1);
		});
	});

	it('shows all-linked state with Check Again when every discovered account is already linked', async () => {
		discoverMock
			.mockResolvedValueOnce([
				{ id: 'ext-1', name: 'Existing Checking', currency: 'USD', balance: '500.00' }
			])
			.mockResolvedValueOnce([
				{ id: 'ext-1', name: 'Existing Checking', currency: 'USD', balance: '500.00' },
				{ id: 'ext-2', name: 'New Savings', currency: 'USD', balance: '1200.00' }
			]);

		const { getByTestId, getByText } = render(SimpleFINSetupModal, {
			props: {
				open: true,
				isDemoMode: false,
				isConnected: true,
				linkedExternalIds: ['ext-1'],
				onClose: vi.fn(),
				onLinked: vi.fn()
			}
		});

		await fireEvent.click(getByTestId('discover-existing-simplefin'));
		await waitFor(() => {
			expect(getByTestId('simplefin-all-linked')).toBeTruthy();
		});

		await fireEvent.click(getByTestId('check-again-btn'));
		await waitFor(() => {
			expect(getByText('1 of 1 new accounts selected')).toBeTruthy();
			expect(getByText('New Savings')).toBeTruthy();
		});
	});

	it('auto-discovers existing accounts on open when autoDiscover is true', async () => {
		discoverMock.mockResolvedValue([
			{ id: 'ext-2', name: 'New Savings', currency: 'USD', balance: '900.00' }
		]);

		const { getByText } = render(SimpleFINSetupModal, {
			props: {
				open: true,
				isDemoMode: false,
				isConnected: true,
				autoDiscover: true,
				onClose: vi.fn(),
				onLinked: vi.fn()
			}
		});

		await waitFor(() => {
			expect(discoverMock).toHaveBeenCalledTimes(1);
			expect(getByText('Map Discovered Accounts')).toBeTruthy();
		});
	});

	it('shows revoked-connection guidance when discoverSimpleFINAccounts fails with rejected credentials', async () => {
		discoverMock.mockRejectedValue(new Error('simplefin rejected the credentials'));

		const { getByTestId, getByRole } = render(SimpleFINSetupModal, {
			props: {
				open: true,
				isDemoMode: false,
				isConnected: true,
				onClose: vi.fn(),
				onLinked: vi.fn()
			}
		});

		await fireEvent.click(getByTestId('discover-existing-simplefin'));
		await waitFor(() => {
			const alertText = getByRole('alert').textContent ?? '';
			expect(alertText).toContain('revoked');
			expect(alertText).not.toContain('single-use');
		});
	});
});
