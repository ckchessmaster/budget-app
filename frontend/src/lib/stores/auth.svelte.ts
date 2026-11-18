import { getUserManager } from '$lib/auth/oidc';
import { browser } from '$app/environment';

export interface UserProfile {
	id: string;
	email: string;
}

function isJwt(token: string | undefined | null): token is string {
	return Boolean(token && token.split('.').length === 3);
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
	private readyPromise: Promise<void> = Promise.resolve();

	constructor() {
		if (browser) {
			this.readyPromise = this.checkUser();
		}
	}

	async ready(): Promise<void> {
		await this.readyPromise;
	}

	async getValidToken(): Promise<string | null> {
		if (!browser) return this.token;
		await this.readyPromise;
		if (!this.token) {
			await this.checkUser();
		}
		return this.token;
	}

	async checkUser(): Promise<void> {
		if (!browser) return;
		try {
			const user = await getUserManager().getUser();
			if (user && !user.expired) {
				this.isAuthenticated = true;
				this.token = selectBestToken(user.access_token, user.id_token);
				this.user = {
					id: user.profile.sub,
					email: user.profile.email || ''
				};
			} else {
				this.isAuthenticated = false;
				this.user = null;
				this.token = null;
			}
		} catch (err) {
			console.error('Failed to load user session:', err);
			this.isAuthenticated = false;
			this.user = null;
			this.token = null;
		}
	}

	async login(): Promise<void> {
		if (!browser) return;
		const userManager = getUserManager();
		await userManager.signinRedirect();
	}

	async handleCallback(): Promise<void> {
		if (!browser) return;
		const userManager = getUserManager();
		const user = await userManager.signinCallback();
		if (user) {
			this.isAuthenticated = true;
			this.token = selectBestToken(user.access_token, user.id_token);
			this.user = {
				id: user.profile.sub,
				email: user.profile.email || ''
			};
		}
	}

	async logout(): Promise<void> {
		if (!browser) return;
		const userManager = getUserManager();
		await userManager.removeUser();
		this.isAuthenticated = false;
		this.user = null;
		this.token = null;
	}
}

export const auth = new AuthStore();
