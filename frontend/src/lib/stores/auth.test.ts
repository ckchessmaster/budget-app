import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('$app/environment', () => ({ browser: true }));

// Importing the store constructs the `auth` singleton, whose constructor reaches
// getUserManager() to wire events. vi.mock factories are hoisted above the imports, so the
// fakes have to be created in a hoisted block as well or that first call reads them from
// their temporal dead zone.
const {
	mockGetUser,
	mockSigninSilent,
	mockSigninCallback,
	mockSigninSilentCallback,
	mockRemoveUser,
	mockSigninRedirect,
	mockEvents
} = vi.hoisted(() => ({
	mockGetUser: vi.fn(),
	mockSigninSilent: vi.fn(),
	mockSigninCallback: vi.fn(),
	mockSigninSilentCallback: vi.fn(),
	mockRemoveUser: vi.fn(),
	mockSigninRedirect: vi.fn(),
	mockEvents: {
		addUserLoaded: vi.fn(),
		addAccessTokenExpired: vi.fn(),
		addAccessTokenExpiring: vi.fn(),
		addSilentRenewError: vi.fn()
	}
}));

vi.mock('$lib/auth/oidc', () => ({
	getUserManager: () => ({
		getUser: mockGetUser,
		signinSilent: mockSigninSilent,
		signinCallback: mockSigninCallback,
		signinSilentCallback: mockSigninSilentCallback,
		removeUser: mockRemoveUser,
		signinRedirect: mockSigninRedirect,
		events: mockEvents
	})
}));

import { AuthStore, RENEW_SKEW_MS } from './auth.svelte';

const LIVE_TOKEN = 'header.live.sig';
const RENEWED_TOKEN = 'header.renewed.sig';

function session(token: string, secondsUntilExpiry: number) {
	return {
		access_token: token,
		id_token: '',
		token_type: 'Bearer',
		scope: 'openid profile email',
		expires_at: Math.floor(Date.now() / 1000) + secondsUntilExpiry,
		expired: secondsUntilExpiry <= 0,
		profile: { sub: 'user-1', email: 'dev@example.com' }
	};
}

async function readyStore() {
	const store = new AuthStore();
	await store.ready();
	return store;
}

beforeEach(() => {
	vi.clearAllMocks();
});

describe('AuthStore.getValidToken', () => {
	it('returns a live bearer without renewing', async () => {
		mockGetUser.mockResolvedValue(session(LIVE_TOKEN, 3600));

		const store = await readyStore();
		await expect(store.getValidToken()).resolves.toBe(LIVE_TOKEN);

		expect(mockSigninSilent).not.toHaveBeenCalled();
		expect(store.isAuthenticated).toBe(true);
		expect(store.sessionExpired).toBe(false);
	});

	it('renews instead of replaying an expired bearer', async () => {
		// This is the reported regression, and it needs the clock to move: the store has to
		// be holding a bearer that is still valid when adopted and dead by the time it is
		// read back. Returning it is what produced the wall of invalid_token responses
		// after a tab sat idle, because this client gets no refresh token.
		mockGetUser.mockResolvedValue(session(LIVE_TOKEN, 3600));
		mockSigninSilent.mockResolvedValue(session(RENEWED_TOKEN, 3600));

		const store = await readyStore();
		await expect(store.getValidToken()).resolves.toBe(LIVE_TOKEN);

		// Fake only the clock: the renewal path awaits real promise resolutions.
		vi.useFakeTimers({ toFake: ['Date'] });
		try {
			vi.advanceTimersByTime(3600 * 1000 + RENEW_SKEW_MS);
			// The store still holds the dead bearer, and so does the session manager.
			mockGetUser.mockResolvedValue(session(LIVE_TOKEN, -1));

			await expect(store.getValidToken()).resolves.toBe(RENEWED_TOKEN);
			expect(store.token).toBe(RENEWED_TOKEN);
			expect(store.sessionExpired).toBe(false);
		} finally {
			vi.useRealTimers();
		}

		expect(mockSigninSilent).toHaveBeenCalledWith({ prompt: 'none' });
	});

	it('drops the session when silent renewal fails', async () => {
		mockGetUser.mockResolvedValue(session(LIVE_TOKEN, -1));
		mockSigninSilent.mockRejectedValue(new Error('state mismatch'));

		const store = new AuthStore();
		await store.ready();

		await expect(store.getValidToken()).resolves.toBeNull();
		expect(store.isAuthenticated).toBe(false);
		expect(store.token).toBeNull();
		expect(store.sessionExpired).toBe(true);
	});

	it('never reports a signed-out visitor as having an expired session', async () => {
		mockGetUser.mockResolvedValue(null);

		const store = await readyStore();

		await expect(store.getValidToken()).resolves.toBeNull();
		expect(store.sessionExpired).toBe(false);
		expect(mockSigninSilent).not.toHaveBeenCalled();
	});

	it('renews ahead of a token that is about to lapse', async () => {
		mockGetUser.mockResolvedValue(session(LIVE_TOKEN, 20));
		mockSigninSilent.mockResolvedValue(session(RENEWED_TOKEN, 3600));

		const store = new AuthStore();
		await store.ready();

		await expect(store.getValidToken()).resolves.toBe(RENEWED_TOKEN);
		expect(mockSigninSilent).toHaveBeenCalledTimes(1);
	});

	it('falls back to the id token when the access token is not a JWT', async () => {
		mockGetUser.mockResolvedValue({
			...session('opaque-access-token', 3600),
			id_token: 'header.idtoken.sig'
		});

		const store = await readyStore();
		await expect(store.getValidToken()).resolves.toBe('header.idtoken.sig');
	});
});

describe('AuthStore session lifecycle', () => {
	it('clears state on logout', async () => {
		mockGetUser.mockResolvedValue(session(LIVE_TOKEN, 3600));

		const store = await readyStore();
		await store.logout();

		expect(mockRemoveUser).toHaveBeenCalled();
		expect(store.isAuthenticated).toBe(false);
		expect(store.token).toBeNull();
		expect(store.sessionExpired).toBe(false);
	});

	it('flags an expired session raised by the token manager', async () => {
		mockGetUser.mockResolvedValue(session(LIVE_TOKEN, 3600));
		const store = await readyStore();

		const expiredHandler = mockEvents.addAccessTokenExpired.mock.calls[0]?.[0];
		expect(expiredHandler).toBeTypeOf('function');
		expiredHandler();

		expect(store.isAuthenticated).toBe(false);
		expect(store.token).toBeNull();
		expect(store.sessionExpired).toBe(true);
	});

	it('does not wire the same listeners twice for one store', async () => {
		mockGetUser.mockResolvedValue(null);
		await readyStore();

		expect(mockEvents.addUserLoaded).toHaveBeenCalledTimes(1);
		expect(mockEvents.addAccessTokenExpired).toHaveBeenCalledTimes(1);
		expect(mockEvents.addSilentRenewError).toHaveBeenCalledTimes(1);
	});

	it('finishes a silent callback through the silent path only', async () => {
		mockGetUser.mockResolvedValue(null);
		mockSigninSilentCallback.mockResolvedValue(undefined);

		const store = await readyStore();
		await store.handleSilentCallback();

		expect(mockSigninSilentCallback).toHaveBeenCalled();
		expect(mockSigninCallback).not.toHaveBeenCalled();
	});

	it('adopts an interactive callback that lands on the silent route', async () => {
		mockGetUser.mockResolvedValue(null);
		mockSigninSilentCallback.mockRejectedValue(new Error('not a silent response'));
		mockSigninCallback.mockResolvedValue(session(RENEWED_TOKEN, 3600));

		const store = await readyStore();
		await store.handleSilentCallback();

		expect(store.isAuthenticated).toBe(true);
		expect(store.token).toBe(RENEWED_TOKEN);
	});

	it('coalesces concurrent getValidToken renewals into a single signinSilent call', async () => {
		mockGetUser.mockResolvedValue(session(LIVE_TOKEN, 20));
		let resolveRenew!: (value: ReturnType<typeof session>) => void;
		mockSigninSilent.mockImplementation(
			() =>
				new Promise((resolve) => {
					resolveRenew = resolve;
				})
		);

		const store = await readyStore();
		const first = store.getValidToken();
		const second = store.getValidToken();

		await vi.waitFor(() => {
			expect(mockSigninSilent).toHaveBeenCalledTimes(1);
		});
		resolveRenew(session(RENEWED_TOKEN, 3600));

		await expect(Promise.all([first, second])).resolves.toEqual([RENEWED_TOKEN, RENEWED_TOKEN]);
		expect(store.token).toBe(RENEWED_TOKEN);
	});

	it('updates store state when the token manager emits userLoaded', async () => {
		mockGetUser.mockResolvedValue(session(LIVE_TOKEN, -1));
		mockSigninSilent.mockRejectedValue(new Error('failed'));

		const store = await readyStore();
		await expect(store.getValidToken()).resolves.toBeNull();
		expect(store.sessionExpired).toBe(true);

		const loadedHandler = mockEvents.addUserLoaded.mock.calls[0]?.[0];
		expect(loadedHandler).toBeTypeOf('function');
		loadedHandler(session(RENEWED_TOKEN, 3600));

		expect(store.isAuthenticated).toBe(true);
		expect(store.token).toBe(RENEWED_TOKEN);
		expect(store.sessionExpired).toBe(false);
	});

	it('skips loading session and wiring events on the silent-callback route', async () => {
		window.history.pushState({}, '', '/auth/silent-callback');
		try {
			const store = new AuthStore();
			await store.ready();

			expect(mockGetUser).not.toHaveBeenCalled();
			expect(mockEvents.addUserLoaded).not.toHaveBeenCalled();
			expect(mockEvents.addAccessTokenExpired).not.toHaveBeenCalled();
			expect(mockEvents.addSilentRenewError).not.toHaveBeenCalled();
		} finally {
			window.history.pushState({}, '', '/');
		}
	});
});
