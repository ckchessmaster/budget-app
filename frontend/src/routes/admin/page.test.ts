import { fireEvent, render, waitFor } from '@testing-library/svelte';
import { beforeEach, describe, expect, it, vi } from 'vitest';

const {
	gotoMock,
	authState,
	fetchInvitesMock,
	createInviteMock,
	revokeInviteMock,
	fetchUsersMock,
	updateStatusMock,
	deleteUserMock,
	fetchWorkspacesMock,
	fetchHealthMock
} = vi.hoisted(() => ({
	gotoMock: vi.fn(),
	authState: {
		isAuthenticated: true,
		isPlatformAdmin: true,
		user: { id: 'admin-1', email: 'admin@dev.local', groups: ['budget-admins'] },
		ready: vi.fn().mockResolvedValue(undefined),
		ensureServerIdentity: vi.fn().mockResolvedValue(undefined)
	},
	fetchInvitesMock: vi.fn(),
	createInviteMock: vi.fn(),
	revokeInviteMock: vi.fn(),
	fetchUsersMock: vi.fn(),
	updateStatusMock: vi.fn(),
	deleteUserMock: vi.fn(),
	fetchWorkspacesMock: vi.fn(),
	fetchHealthMock: vi.fn()
}));

vi.mock('$app/navigation', () => ({ goto: gotoMock }));
vi.mock('$lib/stores/auth.svelte', () => ({ auth: authState }));
vi.mock('$lib/api/admin', async () => {
	const actual = await vi.importActual<typeof import('$lib/api/admin')>('$lib/api/admin');
	return {
		...actual,
		fetchPlatformInvitations: (...args: unknown[]) => fetchInvitesMock(...args),
		createPlatformInvitation: (...args: unknown[]) => createInviteMock(...args),
		revokePlatformInvitation: (...args: unknown[]) => revokeInviteMock(...args),
		fetchPlatformUsers: (...args: unknown[]) => fetchUsersMock(...args),
		updatePlatformUserStatus: (...args: unknown[]) => updateStatusMock(...args),
		deletePlatformUser: (...args: unknown[]) => deleteUserMock(...args),
		fetchAdminWorkspaces: (...args: unknown[]) => fetchWorkspacesMock(...args),
		fetchAdminHealth: (...args: unknown[]) => fetchHealthMock(...args)
	};
});

import AdminLayout from './+layout.svelte';
import AdminPage from './+page.svelte';

describe('/admin layout and dashboard', () => {
	beforeEach(() => {
		vi.clearAllMocks();
		authState.isAuthenticated = true;
		authState.isPlatformAdmin = true;

		HTMLDialogElement.prototype.showModal = vi.fn(function (this: HTMLDialogElement) {
			this.setAttribute('open', '');
		});
		HTMLDialogElement.prototype.close = vi.fn(function (this: HTMLDialogElement) {
			this.removeAttribute('open');
		});

		fetchInvitesMock.mockResolvedValue([
			{
				id: 'inv-1',
				label: 'For Mike',
				status: 'ACTIVE',
				expires_at: new Date(Date.now() + 86400_000).toISOString(),
				created_at: new Date().toISOString()
			}
		]);
		fetchUsersMock.mockResolvedValue([
			{
				user_id: 'admin-1',
				email: 'admin@dev.local',
				display_name: 'Admin',
				is_active: true,
				created_at: new Date().toISOString(),
				owned_workspace_count: 1,
				member_workspace_count: 0
			},
			{
				user_id: 'friend-1',
				email: 'mike@example.com',
				display_name: 'Mike',
				is_active: true,
				created_at: new Date().toISOString(),
				owned_workspace_count: 1,
				member_workspace_count: 1
			}
		]);
		fetchWorkspacesMock.mockResolvedValue([
			{
				workspace_id: 'ws-1',
				workspace_name: "Mike's Budget",
				created_at: new Date().toISOString(),
				member_count: 1,
				owner_email: 'mike@example.com',
				simplefin_status: 'OK'
			}
		]);
		fetchHealthMock.mockResolvedValue({
			status: 'ok',
			checked_at: new Date().toISOString(),
			dependencies: {
				postgres: { status: 'ok', latency_ms: 2 },
				temporal: { status: 'ok', latency_ms: 4 }
			},
			simplefin: {
				configured_workspaces: 1,
				stale_or_errored: []
			},
			temporal_workflows_24h: {
				failed_bank_syncs: 0,
				failed_receipt_workflows: 0,
				failed_invite_emails: 0
			},
			outbound_email_24h: {
				sent: 2,
				failed: 0
			}
		});
	});

	it('redirects non-admins away from /admin layout', async () => {
		authState.isPlatformAdmin = false;
		render(AdminLayout, {
			props: {
				children: (() => {}) as unknown as import('svelte').Snippet
			}
		});
		await waitFor(() => {
			expect(gotoMock).toHaveBeenCalledWith('/');
		});
	});

	it('creates invite, shows copy modal, revokes invite, and toggles/deletes users', async () => {
		createInviteMock.mockResolvedValue({
			id: 'inv-2',
			label: 'For Sarah',
			status: 'ACTIVE',
			expires_at: new Date(Date.now() + 86400_000).toISOString(),
			created_at: new Date().toISOString(),
			invite_url: 'http://localhost:5173/signup?invite=secret-tok-2'
		});
		revokeInviteMock.mockResolvedValue(undefined);
		updateStatusMock.mockResolvedValue({
			user_id: 'friend-1',
			email: 'mike@example.com',
			display_name: 'Mike',
			is_active: false,
			created_at: new Date().toISOString(),
			owned_workspace_count: 1,
			member_workspace_count: 1
		});
		deleteUserMock.mockResolvedValue(undefined);

		const { getByLabelText, getByTestId, getByText } = render(AdminPage);
		await waitFor(() => {
			expect(getByTestId('invite-row-inv-1')).toBeTruthy();
		});

		await fireEvent.input(getByLabelText('Label (optional)'), { target: { value: 'For Sarah' } });
		await fireEvent.submit(getByTestId('generate-invite-btn').closest('form')!);

		await waitFor(() => {
			expect(getByTestId('generated-invite-url').textContent).toContain('secret-tok-2');
		});

		await fireEvent.click(getByTestId('revoke-invite-inv-1'));
		await waitFor(() => {
			expect(revokeInviteMock).toHaveBeenCalledWith('inv-1');
		});

		// Switch to Users & Workspaces tab
		await fireEvent.click(getByTestId('admin-tab-users'));
		await fireEvent.click(getByTestId('toggle-user-friend-1'));
		await waitFor(() => {
			expect(updateStatusMock).toHaveBeenCalledWith('friend-1', false);
		});

		await fireEvent.click(getByTestId('delete-user-friend-1'));
		await fireEvent.input(getByTestId('confirm-delete-email-input'), {
			target: { value: 'mike@example.com' }
		});
		await fireEvent.submit(getByTestId('confirm-delete-user-btn').closest('form')!);
		await waitFor(() => {
			expect(deleteUserMock).toHaveBeenCalledWith('friend-1');
		});

		// Switch to System Health tab
		await fireEvent.click(getByTestId('admin-tab-health'));
		expect(getByTestId('health-overall-status').textContent?.trim().toLowerCase()).toBe('ok');
		expect(getByText('2 sent / 0 failed')).toBeTruthy();
	});
});
