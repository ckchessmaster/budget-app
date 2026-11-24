import { getUserManager } from '$lib/auth/oidc';
import { browser } from '$app/environment';
import type { User } from 'oidc-client-ts';

export interface UserProfile {
	id: string;
	email: string;
}

// Renew this far ahead of expiry so a click never races the token's own deadline.
// Exported so tests assert against the real window rather than a copy of it.
export const RENEW_SKEW_MS = 60_000;

function isJwt(token: string | undefined | null): token is string {
	return Boolean(token && token.split('.').length === 3);
}

/** Whether a session has enough lifetime left to survive the request about to be made. */
function isFresh(session: User): boolean {
	return (session.expires_at ?? 0) * 1000 - Date.now() > RENEW_SKEW_MS;
}

function selectBestToken(accessToken?: string, idToken?: string): string | null {
	if (isJwt(accessToken)) return accessToken;
	if (isJwt(idToken)) return idToken;
	return accessToken || idToken || null;
}

export class AuthStore {
	isAuthenticated = $state(false);
	user = $state<UserProfile | null>(null);
	token = $state<string | null>(null);

	/**
	 * A session existed and could not be renewed, so the user has to sign in again.
	 *
	 * Without this the only signal was the API's own error text surfacing in the UI as
	 * `invalid_token`, which reads like a bug in the app rather than an expired session.
	 */
	sessionExpired = $state(false);

	private expiresAtMs = $state(0);
	private eventsWired = false;
	private readyPromise: Promise<void> = Promise.resolve();

	constructor() {
		if (browser) {
			this.wireEvents();
			this.readyPromise = this.loadSession(false);
		}
	}

	async ready(): Promise<void> {
		await this.readyPromise;
	}

	/**
	 * A bearer usable for the next request, renewing first when the stored one has lapsed.
	 *
	 * Handing back whatever was cached is what turned an idle tab into a wall of
	 * `invalid_token` responses: this client is issued no refresh token, so nothing ever
	 * replaced the expired one and every call kept sending it. Returns null when there is
	 * no renewably-live session, and sets `sessionExpired` so the UI can ask for a sign-in
	 * instead of firing doomed requests and reporting an auth error.
	 */
	async getValidToken(): Promise<string | null> {
		if (!browser) return this.token;
		await this.readyPromise;

		if (this.token && this.expiresAtMs - Date.now() > RENEW_SKEW_MS) {
			return this.token;
		}

		await this.loadSession(true);
		return this.token;
	}

	/** Read the session without attempting a renewal, as done on page load. */
	async checkUser(): Promise<void> {
		if (!browser) return;
		await this.loadSession(false);
	}

	async login(): Promise<void> {
		if (!browser) return;
		await getUserManager().signinRedirect();
	}

	async handleCallback(): Promise<void> {
		if (!browser) return;
		const session = await getUserManager().signinCallback();
		if (session) {
			this.adopt(session);
		}
	}

	/**
	 * Finish a silent renewal that landed in the hidden iframe.
	 *
	 * The iframe's document is a fresh app boot, so this route exists to hand the
	 * provider's response back to the library and stop there. A response that is not a
	 * silent one (an interactive sign-in that reused the redirect target) is processed as
	 * a normal callback rather than dropped.
	 */
	async handleSilentCallback(): Promise<void> {
		if (!browser) return;
		const manager = getUserManager();

		try {
			await manager.signinSilentCallback();
			return;
		} catch {
			const session = await manager.signinCallback();
			if (session) {
				this.adopt(session);
			}
		}
	}

	async logout(): Promise<void> {
		if (!browser) return;
		await getUserManager().removeUser();
		this.clear(false);
	}

	private async loadSession(renew: boolean): Promise<void> {
		const manager = getUserManager();

		let session: User | null = null;
		try {
			session = await manager.getUser();
		} catch (err) {
			console.error('Failed to load user session:', err);
			session = null;
		}

		// A page load adopts whatever is already stored. Only the renewal path demands
		// room to spare, so a session that carries no usable expiry claim is never
		// discarded on boot.
		if (session && !session.expired && (!renew || isFresh(session))) {
			this.adopt(session);
			return;
		}

		// Only a session that already existed is worth renewing. A signed-out visitor must
		// not be pulled through an authorization round-trip by an ordinary API call.
		if (session && renew) {
			try {
				const renewed = await manager.signinSilent({ prompt: 'none' });
				if (renewed && !renewed.expired) {
					this.adopt(renewed);
					return;
				}
			} catch (err) {
				console.warn('Silent session renewal failed:', err);
			}

			if (!session.expired) {
				// Renewal bought nothing, but this token still verifies. Keep working with
				// it rather than signing the user out over one failed iframe round-trip.
				this.adopt(session);
				return;
			}
		}

		this.clear(Boolean(session));
	}

	private adopt(session: User): void {
		this.isAuthenticated = true;
		this.sessionExpired = false;
		this.token = selectBestToken(session.access_token, session.id_token);
		this.expiresAtMs = (session.expires_at ?? 0) * 1000;
		this.user = {
			id: session.profile.sub,
			email: session.profile.email || ''
		};
	}

	private clear(expired: boolean): void {
		this.isAuthenticated = false;
		this.user = null;
		this.token = null;
		this.expiresAtMs = 0;
		this.sessionExpired = expired;
	}

	private wireEvents(): void {
		if (this.eventsWired) return;
		this.eventsWired = true;

		const manager = getUserManager();

		// automaticSilentRenew covers proactive renewal; these two make sure a session that
		// genuinely died stops pretending, rather than lingering as a stale bearer.
		manager.events.addAccessTokenExpired(() => {
			this.clear(true);
		});
		manager.events.addSilentRenewError(() => {
			this.sessionExpired = true;
		});
	}
}

export const auth = new AuthStore();
