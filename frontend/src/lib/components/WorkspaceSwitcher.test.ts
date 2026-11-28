import { fireEvent, render } from '@testing-library/svelte';
import { beforeEach, describe, expect, it, vi } from 'vitest';

const { switchWorkspaceMock, createPersonalMock, authState, demoState, workspaceState } = vi.hoisted(() => ({
	switchWorkspaceMock: vi.fn(),
	createPersonalMock: vi.fn(),
	authState: { isAuthenticated: true },
	demoState: { isDemo: false },
	workspaceState: {
		workspaces: [
			{ id: 'ws-personal', name: "Chris's Budget", role: 'owner', is_demo: false },
			{ id: 'ws-household', name: 'Kingdon Household', role: 'member', is_demo: false }
		],
		activeWorkspaceId: 'ws-personal',
		activeWorkspace: { id: 'ws-personal', name: "Chris's Budget", role: 'owner', is_demo: false },
		currentRole: 'owner',
		canCreatePersonalWorkspace: false,
		switchWorkspace: vi.fn(),
		createPersonal: vi.fn()
	}
}));

workspaceState.switchWorkspace = switchWorkspaceMock;
workspaceState.createPersonal = createPersonalMock;

vi.mock('$lib/stores/auth.svelte', () => ({ auth: authState }));
vi.mock('$lib/stores/demo.svelte', () => ({ demo: demoState }));
vi.mock('$lib/stores/workspace.svelte', () => ({ workspace: workspaceState }));

import WorkspaceSwitcher from './WorkspaceSwitcher.svelte';

describe('WorkspaceSwitcher', () => {
	beforeEach(() => {
		vi.clearAllMocks();
		authState.isAuthenticated = true;
		demoState.isDemo = false;
		workspaceState.activeWorkspaceId = 'ws-personal';
		workspaceState.activeWorkspace = workspaceState.workspaces[0];
		workspaceState.currentRole = 'owner';
		workspaceState.canCreatePersonalWorkspace = false;
	});

	it('renders active workspace and switches on click', async () => {
		const { getByTestId } = render(WorkspaceSwitcher);
		const btn = getByTestId('workspace-switcher-btn');
		expect(btn.textContent).toContain("Chris's Budget");

		await fireEvent.click(btn);
		const option = getByTestId('workspace-option-ws-household');
		await fireEvent.click(option);
		expect(switchWorkspaceMock).toHaveBeenCalledWith('ws-household');
	});

	it('shows role pill when active workspace is member or read_only', () => {
		workspaceState.activeWorkspaceId = 'ws-household';
		workspaceState.activeWorkspace = workspaceState.workspaces[1];
		workspaceState.currentRole = 'member';

		const { getByTestId } = render(WorkspaceSwitcher);
		expect(getByTestId('workspace-role-pill').textContent).toContain('Member');
	});

	it('offers creating personal workspace when user only joined via invite', async () => {
		workspaceState.canCreatePersonalWorkspace = true;
		const { getByTestId } = render(WorkspaceSwitcher);

		await fireEvent.click(getByTestId('workspace-switcher-btn'));
		expect(getByTestId('create-personal-workspace-btn')).toBeTruthy();
	});
});
