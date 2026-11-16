import { browser } from '$app/environment';

export interface AppConfig {
	oidcAuthority: string;
	oidcClientId: string;
	apiBaseUrl: string;
}

declare global {
	interface Window {
		__APP_CONFIG__?: Partial<AppConfig>;
	}
}

export function getAppConfig(): AppConfig {
	const runtime = browser ? window.__APP_CONFIG__ : undefined;
	return {
		oidcAuthority: runtime?.oidcAuthority ?? 'http://localhost:5556/dex',
		oidcClientId: runtime?.oidcClientId ?? 'budget-app-client',
		apiBaseUrl: runtime?.apiBaseUrl ?? 'http://localhost:8080'
	};
}
