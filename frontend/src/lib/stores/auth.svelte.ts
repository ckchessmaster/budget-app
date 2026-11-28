import { getUserManager } from '$lib/auth/oidc';
import { browser } from '$app/environment';
import { getAppConfig } from '$lib/config';
import { BudgetLocalDB } from '$lib/offline/db';
import type { User } from 'oidc-client-ts';

export interface UserProfile {
	id: string;
	email: string;
	name?: string;
	groups?: string[];
}

// Renew this far ahead of expiry so a click never races the token's own deadline.
// Exported so tests assert against the real window rather than a copy of it.
export const RENEW_SKEW_MS = 60_000;

const DEFAULT_ADMIN_GROUP = 'budget-admins';

function isOfflineNow(): boolean {
	return browser && typeof navigator !== 'undefined' && navigator.onLine === false;
}

function isJwt(token: string | undefined | null): token is string {
	return Boolean(token && token.split('.').length === 3);
}

function decodeJwtGroups(token: string | undefined | null): string[] {
	if (!isJwt(token) || typeof atob !== 'function') return [];
	try {
		const payloadB64 = token.split('.')[1].replace(/-/g, '+').replace(/_/g, '/');
		const padded = payloadB64.padEnd(payloadB64.length + ((4 - (payloadB64.length % 4)) % 4), '=');
		const parsed = JSON.parse(atob(padded)) as { groups?: unknown };
		if (!Array.isArray(parsed.groups)) return [];
		return parsed.groups
			.filter((g): g is string => typeof g === 'string' && g.trim().length > 0)
			.map((g) => g.trim());
	} catch {
		return [];
	}
}

function extractGroups(session: User): string[] {
	const profileGroups = (session.profile as Record<string, unknown> | undefined)?.groups;
	if (Array.isArray(profileGroups)) {
		const normalized = profileGroups
			.filter((g): g is string => typeof g === 'string' && g.trim().length > 0)
			.map((g) => g.trim());
		if (normalized.length > 0) return normalized;
	}
	const fromAccess = decodeJwtGroups(session.access_token);
	if (fromAccess.length > 0) return fromAccess;
	return decodeJwtGroups(session.id_token);
}

/** Whether a session has enough lifetime left to survive the request about to be made. */
function isFresh(session: User): boolean {
	return (session.expires_at ?? 0) * 1000 - Date.now() > RENEW_SKEW_MS;
}

function isSilentCallbackPath(): boolean {
	return (
		browser &&
		typeof window !== 'undefined' &&
		window.location.pathname.startsWith('/auth/silent-callback')
	);
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
	isPlatformAdmin = $state(false);
	adminStatusLoaded = $state(false);
	accountDisabled = $state(false);

	/**
	 * A session existed and could not be renewed, so the user has to sign in again.
	 *
	 * Without this the only signal was the API's own error text surfacing in the UI as
	 * `invalid_token`, which reads like a bug in the app rather than an expired session.
	 */
	sessionExpired = $state(false);

	/**
	 * True when a persisted OIDC session profile is available locally (even if its token
	 * expired while offline), allowing offline access to cached data and the local queue.
	 */
	hasCachedSession = $state(false);
	cachedUser = $state<UserProfile | null>(null);

	private expiresAtMs = $state(0);
	private eventsWired = false;
	private readyPromise: Promise<void> = Promise.resolve();
	private renewPromise: Promise<void> | null = null;
	private mePromise: Promise<void> | null = null;

	get canAccessOffline(): boolean {
		return this.isAuthenticated || (this.hasCachedSession && isOfflineNow());
	}

	constructor() {
		if (browser && !isSilentCallbackPath()) {
			this.wireEvents();
			this.readyPromise = this.loadSession(false);
		}
	}

	async ready(): Promise<void> {
		await this.readyPromise;
	}

	/**
	 * Resolves server-authoritative admin & active status from GET /api/v1/auth/me.
	 * Keeps the JWT groups hint when offline.
	 */
	async refreshServerIdentity(): Promise<void> {
		if (!browser || !this.token || typeof fetch !== 'function') {
			this.adminStatusLoaded = true;
			return;
		}
		if (this.mePromise) {
			return this.mePromise;
		}

		const bearer = this.token;
		this.mePromise = (async () => {
			try {
				const { apiBaseUrl } = getAppConfig();
				const res = await fetch(`${apiBaseUrl}/api/v1/auth/me`, {
					headers: { Authorization: `Bearer ${bearer}` }
				});
				if (res.ok) {
					const data = (await res.json()) as {
						is_platform_admin?: boolean;
						is_active?: boolean;
					};
					this.isPlatformAdmin = Boolean(data.is_platform_admin);
					this.accountDisabled = data.is_active === false;
				} else if (res.status === 403) {
					const errBody = (await res.json().catch(() => ({}))) as { error?: string };
					if (errBody.error === 'account_disabled') {
						this.accountDisabled = true;
						this.isPlatformAdmin = false;
					}
				}
			} catch {
				// Offline or transient network error; preserve JWT-derived admin hint.
			} finally {
				this.adminStatusLoaded = true;
				this.mePromise = null;
			}
		})();

		return this.mePromise;
	}

	async ensureServerIdentity(): Promise<void> {
		await this.readyPromise;
		if (!this.isAuthenticated) return;
		if (!this.adminStatusLoaded) {
			await this.refreshServerIdentity();
		}
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

		if (!this.renewPromise) {
			this.renewPromise = this.loadSession(true).finally(() => {
				this.renewPromise = null;
			});
		}
		await this.renewPromise;
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
		await new BudgetLocalDB().clearCaches().catch(() => {});
		this.clear(false);
	}

	/**
	 * Attempts to silently restore an expired cached session once connectivity returns.
	 */
	async tryRestoreSession(): Promise<boolean> {
		if (!browser) return this.isAuthenticated;
		await this.readyPromise;
		if (this.isAuthenticated && this.expiresAtMs - Date.now() > RENEW_SKEW_MS) {
			return true;
		}
		if (!this.hasCachedSession || isOfflineNow()) {
			return this.isAuthenticated;
		}
		if (!this.renewPromise) {
			this.renewPromise = this.loadSession(true).finally(() => {
				this.renewPromise = null;
			});
		}
		await this.renewPromise;
		return this.isAuthenticated;
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

		if (session) {
			this.hasCachedSession = true;
			this.cachedUser = {
				id: session.profile.sub,
				email: session.profile.email || '',
				name: session.profile.name || '',
				groups: extractGroups(session)
			};
		}

		// A page load adopts whatever is already stored when still live. Only the renewal
		// path demands room to spare, so a session that carries no usable expiry claim is
		// never discarded on boot.
		if (session && !session.expired && (!renew || isFresh(session))) {
			this.adopt(session);
			return;
		}

		// When a stored session has already expired (e.g. PWA reopened the next day) or is
		// nearing expiry, attempt renewal via refresh token or silent SSO—unless offline,
		// where hanging on an unreachable IdP would block offline cache access.
		if (session && (renew || session.expired) && !isOfflineNow()) {
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
				// it rather than signing the user out over one failed round-trip.
				this.adopt(session);
				return;
			}
		} else if (session && !session.expired) {
			this.adopt(session);
			return;
		}

		this.clear(Boolean(session));
	}

	private adopt(session: User): void {
		const groups = extractGroups(session);
		this.isAuthenticated = true;
		this.sessionExpired = false;
		this.hasCachedSession = true;
		this.token = selectBestToken(session.access_token, session.id_token);
		this.expiresAtMs = (session.expires_at ?? 0) * 1000;
		const profile: UserProfile = {
			id: session.profile.sub,
			email: session.profile.email || '',
			name: session.profile.name || '',
			groups
		};
		this.user = profile;
		this.cachedUser = profile;
		this.isPlatformAdmin = groups.includes(DEFAULT_ADMIN_GROUP);
		this.adminStatusLoaded = false;
		this.accountDisabled = false;
		if (browser && !isSilentCallbackPath()) {
			void this.refreshServerIdentity();
		}
	}

	private clear(expired: boolean): void {
		this.isAuthenticated = false;
		this.user = null;
		this.token = null;
		this.expiresAtMs = 0;
		this.sessionExpired = expired;
		this.isPlatformAdmin = false;
		this.adminStatusLoaded = false;
		this.accountDisabled = false;
		if (!expired) {
			this.hasCachedSession = false;
			this.cachedUser = null;
		}
	}

	private wireEvents(): void {
		if (this.eventsWired) return;
		this.eventsWired = true;

		const manager = getUserManager();

		// automaticSilentRenew covers proactive renewal; these keep store state synced on
		// background renewals and stop pretending if a session genuinely died.
		manager.events.addUserLoaded((session) => {
			this.adopt(session);
		});
		manager.events.addAccessTokenExpired(() => {
			this.clear(true);
		});
		manager.events.addSilentRenewError(() => {
			this.sessionExpired = true;
		});

		if (typeof window !== 'undefined') {
			window.addEventListener('online', () => {
				if (this.sessionExpired && this.hasCachedSession) {
					void this.tryRestoreSession();
				}
			});
		}
	}
}

export const auth = new AuthStore();
