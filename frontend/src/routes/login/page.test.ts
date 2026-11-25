import { fireEvent, render, waitFor } from '@testing-library/svelte';
import { beforeEach, describe, expect, it, vi } from 'vitest';

const { loginMock, gotoMock, authState } = vi.hoisted(() => ({
	loginMock: vi.fn(),
	gotoMock: vi.fn(),
	authState: { isAuthenticated: false }
}));

vi.mock('$lib/stores/auth.svelte', () => ({
	auth: {
		get isAuthenticated() {
			return authState.isAuthenticated;
		},
		login: loginMock
	}
}));

vi.mock('$app/navigation', () => ({
	goto: gotoMock
}));

import LoginPage from './+page.svelte';

describe('Login page', () => {
	beforeEach(() => {
		authState.isAuthenticated = false;
		loginMock.mockReset();
		gotoMock.mockReset();
	});

	it('renders generic Sign In screen without Dex or local dev credentials', () => {
		const { container, getByRole, getByText } = render(LoginPage);

		expect(getByText('Welcome Back')).toBeTruthy();
		expect(getByText('Sign in to continue to your budget')).toBeTruthy();
		expect(getByRole('button', { name: 'Sign In' })).toBeTruthy();

		const text = container.textContent ?? '';
		expect(text).not.toMatch(/dex/i);
		expect(text).not.toMatch(/oidc/i);
		expect(text).not.toContain('Local Dev Credentials');
		expect(text).not.toContain('admin@dev.local');
		expect(text).not.toContain('password');
	});

	it('calls auth.login and shows generic Redirecting... state on click', async () => {
		loginMock.mockImplementation(() => new Promise(() => {}));

		const { getByRole, getByText } = render(LoginPage);
		await fireEvent.click(getByRole('button', { name: 'Sign In' }));

		await waitFor(() => {
			expect(loginMock).toHaveBeenCalledTimes(1);
			expect(getByText('Redirecting...')).toBeTruthy();
		});
	});
});
