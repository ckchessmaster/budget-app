import { fireEvent, render, waitFor } from '@testing-library/svelte';
import { beforeEach, describe, expect, it, vi } from 'vitest';

const {
	fetchMembersMock,
	fetchInvitesMock,
	createInviteMock,
	resendInviteMock,
	revokeInviteMock,
	updateRoleMock,
	removeMemberMock,
	workspaceState,
	demoState
} = vi.hoisted(() => ({
	fetchMembersMock: vi.fn(),
	fetchInvitesMock: vi.fn(),
	createInviteMock: vi.fn(),
	resendInviteMock: vi.fn(),
	revokeInviteMock: vi.fn(),
	updateRoleMock: vi.fn(),
	removeMemberMock: vi.fn(),
	workspaceState: {
		activeWorkspaceId: 'ws-1',
		currentRole: 'owner',
		isOwner: true,
		loaded: true,
		load: vi.fn().mockResolvedValue(undefined)
	},
	demoState: {
		isDemo: false,
		dataVersion: 0,
		load: vi.fn().mockResolvedValue(undefined)
	}
}));

vi.mock('$lib/api/workspaces', () => ({
	fetchWorkspaceMembers: (...args: unknown[]) => fetchMembersMock(...args),
	fetchWorkspaceInvitations: (...args: unknown[]) => fetchInvitesMock(...args),
	createWorkspaceInvitation: (...args: unknown[]) => createInviteMock(...args),
	resendWorkspaceInvitation: (...args: unknown[]) => resendInviteMock(...args),
	revokeWorkspaceInvitation: (...args: unknown[]) => revokeInviteMock(...args),
	updateMemberRole: (...args: unknown[]) => updateRoleMock(...args),
	removeWorkspaceMember: (...args: unknown[]) => removeMemberMock(...args)
}));
vi.mock('$lib/stores/workspace.svelte', () => ({ workspace: workspaceState }));
vi.mock('$lib/stores/demo.svelte', () => ({ demo: demoState }));

import HouseholdSection from './HouseholdSection.svelte';

describe('HouseholdSection', () => {
	beforeEach(() => {
		vi.clearAllMocks();
		workspaceState.isOwner = true;
		workspaceState.currentRole = 'owner';
		demoState.isDemo = false;

		fetchMembersMock.mockResolvedValue([
			{
				user_id: 'u-owner',
				email: 'chris@example.com',
				display_name: 'Chris',
				role: 'owner',
				created_at: '2026-11-01T00:00:00Z',
				is_current_user: true
			},
			{
				user_id: 'u-member',
				email: 'jane@example.com',
				display_name: 'Jane',
				role: 'member',
				created_at: '2026-11-02T00:00:00Z',
				is_current_user: false
			}
		]);
		fetchInvitesMock.mockResolvedValue([
			{
				id: 'inv-1',
				workspace_id: 'ws-1',
				email: 'pending@example.com',
				role: 'read_only',
				status: 'PENDING',
				expires_at: new Date(Date.now() + 3 * 86400_000).toISOString(),
				created_at: '2026-11-26T00:00:00Z'
			}
		]);
	});

	it('renders members, pending invites, and sends new invitation as owner', async () => {
		createInviteMock.mockResolvedValue({
			id: 'inv-2',
			workspace_id: 'ws-1',
			email: 'alex@example.com',
			role: 'member',
			status: 'PENDING',
			expires_at: new Date(Date.now() + 7 * 86400_000).toISOString(),
			created_at: '2026-11-27T00:00:00Z'
		});

		const { getByLabelText, getByTestId, getAllByTestId } = render(HouseholdSection);

		await waitFor(() => {
			expect(getAllByTestId('household-member-row')).toHaveLength(2);
			expect(getAllByTestId('pending-invite-row')).toHaveLength(1);
		});

		const input = getByLabelText('Email Address') as HTMLInputElement;
		await fireEvent.input(input, { target: { value: 'alex@example.com' } });
		await fireEvent.click(getByTestId('send-invite-btn'));

		await waitFor(() => {
			expect(createInviteMock).toHaveBeenCalledWith('alex@example.com', 'member');
			expect(getAllByTestId('pending-invite-row')).toHaveLength(2);
		});
	});

	it('hides invite controls and shows Leave Workspace for non-owner', async () => {
		workspaceState.isOwner = false;
		workspaceState.currentRole = 'member';
		fetchMembersMock.mockResolvedValue([
			{
				user_id: 'u-owner',
				email: 'chris@example.com',
				display_name: 'Chris',
				role: 'owner',
				created_at: '2026-11-01T00:00:00Z',
				is_current_user: false
			},
			{
				user_id: 'u-member',
				email: 'jane@example.com',
				display_name: 'Jane',
				role: 'member',
				created_at: '2026-11-02T00:00:00Z',
				is_current_user: true
			}
		]);

		const { queryByTestId, getByTestId } = render(HouseholdSection);
		await waitFor(() => {
			expect(getByTestId('leave-workspace-btn')).toBeTruthy();
		});
		expect(queryByTestId('send-invite-btn')).toBeNull();
	});
});
