import { fireEvent, render, waitFor } from '@testing-library/svelte';
import { beforeEach, describe, expect, it, vi } from 'vitest';

const { validateMock, redeemMock, pageState } = vi.hoisted(() => ({
	validateMock: vi.fn(),
	redeemMock: vi.fn(),
	pageState: {
		url: new URL('http://localhost:5173/signup?invite=raw-invite-tok')
	}
}));

vi.mock('$app/state', () => ({ page: pageState }));
vi.mock('$lib/api/admin', async () => {
	const actual = await vi.importActual<typeof import('$lib/api/admin')>('$lib/api/admin');
	return {
		...actual,
		validatePlatformInvite: (...args: unknown[]) => validateMock(...args),
		redeemPlatformInvite: (...args: unknown[]) => redeemMock(...args)
	};
});

import { ApiError } from '$lib/api/workspaces';
import SignupPage from './+page.svelte';

describe('/signup page', () => {
	beforeEach(() => {
		vi.clearAllMocks();
		pageState.url = new URL('http://localhost:5173/signup?invite=raw-invite-tok');
	});

	it('validates invite token, pre-fills optional email, and submits redemption', async () => {
		validateMock.mockResolvedValue({
			valid: true,
			expires_at: new Date(Date.now() + 86400_000).toISOString(),
			email: 'mike@example.com'
		});
		redeemMock.mockResolvedValue({
			setup_url: 'https://auth.example.com/recovery/123'
		});

		const assignSpy = vi.fn();
		Object.defineProperty(window, 'location', {
			value: { ...window.location, assign: assignSpy },
			writable: true
		});

		const { getByLabelText, getByTestId } = render(SignupPage);
		await waitFor(() => {
			expect(getByTestId('signup-submit-btn')).toBeTruthy();
		});

		const emailInput = getByLabelText('Email Address') as HTMLInputElement;
		expect(emailInput.value).toBe('mike@example.com');

		await fireEvent.input(getByLabelText('Full Name'), { target: { value: 'Mike Johnson' } });
		await fireEvent.input(getByLabelText('Username'), { target: { value: 'mikej' } });
		await fireEvent.submit(getByTestId('signup-submit-btn').closest('form')!);

		await waitFor(() => {
			expect(redeemMock).toHaveBeenCalledWith({
				token: 'raw-invite-tok',
				name: 'Mike Johnson',
				email: 'mike@example.com',
				username: 'mikej'
			});
			expect(assignSpy).toHaveBeenCalledWith('https://auth.example.com/recovery/123');
		});
	});

	it('shows error state when invite token is expired or already redeemed', async () => {
		validateMock.mockRejectedValue(
			new ApiError(409, 'invitation_already_redeemed', 'invitation_already_redeemed')
		);

		const { getByTestId } = render(SignupPage);
		await waitFor(() => {
			expect(getByTestId('signup-invite-error').textContent).toContain('already been redeemed');
		});
	});
});
