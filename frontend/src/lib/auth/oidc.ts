import { UserManager, WebStorageStateStore, type UserManagerSettings } from 'oidc-client-ts';
import { browser } from '$app/environment';
import { getAppConfig } from '$lib/config';

const appConfig = getAppConfig();

export const oidcSettings: UserManagerSettings = {
	authority: appConfig.oidcAuthority,
	client_id: appConfig.oidcClientId,
	redirect_uri: browser ? `${window.location.origin}/auth/callback` : 'http://localhost:5173/auth/callback',
	response_type: 'code',
	scope: 'openid profile email',
	userStore: browser ? new WebStorageStateStore({ store: window.sessionStorage }) : undefined,
	loadUserInfo: true,
	// Renewal, not reuse. This client is issued no refresh token (there is no
	// offline_access in the scope above), so the only way to replace an expired access
	// token is a silent authorization against the existing single-sign-on session.
	// Without any renewal path the app held on to the token it had, and every call
	// started answering invalid_token once it lapsed.
	//
	// The identity provider must allowlist the silent redirect target below as a redirect URI
	// for this application; otherwise the silent attempt fails and the user is asked to
	// sign in again, which is the correct outcome but not a quiet one.
	automaticSilentRenew: true,
	// Bounds how long a request can stall behind a silent renewal whose iframe never reports back.
	silentRequestTimeoutInSeconds: 15,
	silent_redirect_uri: browser
		? `${window.location.origin}/auth/silent-callback`
		: 'http://localhost:5173/auth/silent-callback'
};

let userManagerInstance: UserManager | null = null;

export function getUserManager(): UserManager {
	if (!userManagerInstance) {
		userManagerInstance = new UserManager(oidcSettings);
	}
	return userManagerInstance;
}
