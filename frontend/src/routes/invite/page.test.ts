import { fireEvent, render, waitFor } from '@testing-library/svelte';
import { beforeEach, describe, expect, it, vi } from 'vitest';

const {
	previewMock,
	acceptMock,
	gotoMock,
	setPendingMock,
	authState,
	workspaceState,
	demoState,
	pageState
} = vi.hoisted(() => ({
	previewMock: vi.fn(),
	acceptMock: vi.fn(),
	gotoMock: vi.fn(),
	setPendingMock: vi.fn(),
	authState: {
		isAuthenticated: false,
		ready: vi.fn().mockResolvedValue(undefined),
		login: vi.fn().mockResolvedValue(undefined),
		logout: vi.fn().mockResolvedValue(undefined)
	},
	workspaceState: {
		load: vi.fn().mockResolvedValue(undefined),
		resetOnLogout: vi.fn()
	},
	demoState: {
		dataVersion: 0,
		load: vi.fn().mockResolvedValue(undefined),
		resetOnLogout: vi.fn()
	},
	pageState: {
		url: new URL('http://localhost:5173/invite?token=raw-tok-123')
	}
}));

vi.mock('$app/navigation', () => ({ goto: gotoMock }));
vi.mock('$app/state', () => ({ page: pageState }));
vi.mock('$lib/stores/auth.svelte', () => ({ auth: authState }));
vi.mock('$lib/stores/demo.svelte', () => ({ demo: demoState }));
vi.mock('$lib/stores/workspace.svelte', () => ({
	workspace: workspaceState,
	setPendingJoinedWorkspace: setPendingMock
}));
vi.mock('$lib/api/workspaces', async () => {
	const actual = await vi.importActual<typeof import('$lib/api/workspaces')>('$lib/api/workspaces');
	return {
		...actual,
		previewInvitation: (...args: unknown[]) => previewMock(...args),
		acceptInvitation: (...args: unknown[]) => acceptMock(...args)
	};
});

import { ApiError } from '$lib/api/workspaces';
import InvitePage from './+page.svelte';

describe('/invite page', () => {
	beforeEach(() => {
		vi.clearAllMocks();
		authState.isAuthenticated = false;
		pageState.url = new URL('http://localhost:5173/invite?token=raw-tok-123');
	});

	it('renders preview and submits accept for existing user when signed in', async () => {
		authState.isAuthenticated = true;
		previewMock.mockResolvedValue({
			workspace_name: 'Kingdon Household',
			email: 'jane@example.com',
			role: 'member',
			status: 'PENDING',
			expires_at: new Date(Date.now() + 86400_000).toISOString(),
			email_mismatch: false
		});
		acceptMock.mockResolvedValue({
			workspace_id: 'ws-household',
			workspace_name: 'Kingdon Household',
			role: 'member',
			is_new_user: false,
			redirect_url: 'http://localhost:5173/login'
		});

		const { getByTestId, getByText } = render(InvitePage);
		await waitFor(() => {
			expect(getByText('Kingdon Household')).toBeTruthy();
		});

		await fireEvent.click(getByTestId('accept-invite-btn'));
		await waitFor(() => {
			expect(acceptMock).toHaveBeenCalledWith('raw-tok-123', '');
			expect(setPendingMock).toHaveBeenCalledWith('ws-household');
			expect(gotoMock).toHaveBeenCalledWith('/budget');
		});
	});

	it('displays expired and email mismatch error states clearly', async () => {
		previewMock.mockResolvedValueOnce({
			workspace_name: 'Kingdon Household',
			email: 'jane@example.com',
			role: 'member',
			status: 'EXPIRED',
			expires_at: '2026-01-01T00:00:00Z',
			email_mismatch: false
		});

		const { getByTestId } = render(InvitePage);
		await waitFor(() => {
			expect(getByTestId('invite-error').textContent).toContain('expired');
		});

		previewMock.mockRejectedValueOnce(new ApiError(403, 'email_mismatch', 'email_mismatch'));
		const second = render(InvitePage);
		await waitFor(() => {
			expect(second.getByTestId('switch-account-btn')).toBeTruthy();
		});
	});
});
