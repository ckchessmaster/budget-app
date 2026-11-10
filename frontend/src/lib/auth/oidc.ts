import { UserManager, WebStorageStateStore, type UserManagerSettings } from 'oidc-client-ts';
import { browser } from '$app/environment';

export const oidcSettings: UserManagerSettings = {
	authority: 'http://localhost:5556/dex',
	client_id: 'budget-app-client',
	redirect_uri: browser ? `${window.location.origin}/auth/callback` : 'http://localhost:5173/auth/callback',
	response_type: 'code',
	scope: 'openid profile email',
	userStore: browser ? new WebStorageStateStore({ store: window.sessionStorage }) : undefined,
	loadUserInfo: true
};

let userManagerInstance: UserManager | null = null;

export function getUserManager(): UserManager {
	if (!userManagerInstance) {
		userManagerInstance = new UserManager(oidcSettings);
	}
	return userManagerInstance;
}
