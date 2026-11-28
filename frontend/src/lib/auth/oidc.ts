import { UserManager, WebStorageStateStore, type UserManagerSettings } from 'oidc-client-ts';
import { browser } from '$app/environment';
import { getAppConfig } from '$lib/config';

const appConfig = getAppConfig();

export const oidcSettings: UserManagerSettings = {
	authority: appConfig.oidcAuthority,
	client_id: appConfig.oidcClientId,
	redirect_uri: browser ? `${window.location.origin}/auth/callback` : 'http://localhost:5173/auth/callback',
	response_type: 'code',
	scope: 'openid profile email groups offline_access',
	userStore: browser ? new WebStorageStateStore({ store: window.localStorage }) : undefined,
	loadUserInfo: true,
	// Persist the session in localStorage (survives mobile PWA restarts) and request
	// offline_access so providers issue a refresh token when allowed. When a refresh token
	// is present, oidc-client-ts renews via the token endpoint directly; otherwise it falls
	// back to a silent authorization iframe against the existing SSO session.
	//
	// The identity provider must allowlist the silent redirect target below as a redirect URI
	// for this application as a fallback; otherwise silent iframe renewal fails and the user
	// is asked to sign in again.
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
		const isSilentCallback =
			browser && window.location.pathname.startsWith('/auth/silent-callback');
		userManagerInstance = new UserManager(
			isSilentCallback ? { ...oidcSettings, automaticSilentRenew: false } : oidcSettings
		);
	}
	return userManagerInstance;
}
