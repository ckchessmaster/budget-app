import { getUserManager } from '$lib/auth/oidc';
import { browser } from '$app/environment';

export interface UserProfile {
	id: string;
	email: string;
}

export class AuthStore {
	isAuthenticated = $state(false);
	user = $state<UserProfile | null>(null);
	token = $state<string | null>(null);

	constructor() {
		if (browser) {
			this.checkUser();
		}
	}

	async checkUser(): Promise<void> {
		if (!browser) return;
		try {
			const user = await getUserManager().getUser();
			if (user && !user.expired) {
				this.isAuthenticated = true;
				this.token = user.access_token || user.id_token || null;
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
			this.token = user.access_token || user.id_token || null;
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
