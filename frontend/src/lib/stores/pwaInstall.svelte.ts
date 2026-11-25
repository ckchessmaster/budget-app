import { isStandaloneMode } from '$lib/notifications/push';

export const PWA_INSTALL_DISMISSED_KEY = 'budget_pwa_install_dismissed';

export interface BeforeInstallPromptEvent extends Event {
	readonly platforms?: string[];
	readonly userChoice: Promise<{ outcome: 'accepted' | 'dismissed'; platform: string }>;
	prompt(): Promise<void>;
}

interface NavigatorWithInstall extends Navigator {
	install?: () => Promise<void>;
}

function readDismissed(): boolean {
	if (typeof window === 'undefined') return false;
	try {
		return window.localStorage.getItem(PWA_INSTALL_DISMISSED_KEY) === 'true';
	} catch {
		return false;
	}
}

function writeDismissed(value: boolean): void {
	if (typeof window === 'undefined') return;
	try {
		if (value) {
			window.localStorage.setItem(PWA_INSTALL_DISMISSED_KEY, 'true');
		} else {
			window.localStorage.removeItem(PWA_INSTALL_DISMISSED_KEY);
		}
	} catch {
		// localStorage unavailable
	}
}

function hasNavigatorInstallSupport(): boolean {
	if (typeof Navigator === 'undefined') return false;
	return typeof (Navigator.prototype as NavigatorWithInstall).install === 'function';
}

class PwaInstallStore {
	private deferredPrompt = $state<BeforeInstallPromptEvent | null>(null);
	private supportsWebInstall = $state(false);
	private isStandalone = $state(false);
	private installed = $state(false);
	private dismissed = $state(false);
	private _installing = $state(false);
	private _statusMessage = $state<string | null>(null);

	get canInstall(): boolean {
		if (this.isStandalone || this.installed) return false;
		return this.supportsWebInstall || this.deferredPrompt !== null;
	}

	get shouldShowBanner(): boolean {
		return this.canInstall && !this.dismissed;
	}

	get installing(): boolean {
		return this._installing;
	}

	get statusMessage(): string | null {
		return this._statusMessage;
	}

	init(): () => void {
		if (typeof window === 'undefined') return () => {};

		this.isStandalone = isStandaloneMode(window);
		this.supportsWebInstall = hasNavigatorInstallSupport();
		this.dismissed = readDismissed();

		const onBeforeInstallPrompt = (event: Event) => {
			event.preventDefault();
			this.deferredPrompt = event as BeforeInstallPromptEvent;
			this._statusMessage = null;
		};

		const onAppInstalled = () => {
			this.installed = true;
			this.deferredPrompt = null;
			this._statusMessage = 'Budget App was installed.';
		};

		window.addEventListener('beforeinstallprompt', onBeforeInstallPrompt);
		window.addEventListener('appinstalled', onAppInstalled);

		return () => {
			window.removeEventListener('beforeinstallprompt', onBeforeInstallPrompt);
			window.removeEventListener('appinstalled', onAppInstalled);
		};
	}

	async promptInstall(): Promise<'installed' | 'canceled' | 'error' | 'unavailable'> {
		if (this._installing) return 'unavailable';

		const navInstall =
			this.supportsWebInstall && typeof navigator !== 'undefined'
				? (navigator as NavigatorWithInstall).install
				: undefined;

		if (typeof navInstall === 'function') {
			this._installing = true;
			this._statusMessage = null;
			try {
				// Call directly inside user gesture without prior awaits.
				await navInstall.call(navigator);
				this.installed = true;
				this.deferredPrompt = null;
				this._statusMessage = 'The app was installed or opened.';
				return 'installed';
			} catch (err) {
				if (err instanceof Error && err.name === 'AbortError') {
					this._statusMessage = 'Installation was canceled or could not complete.';
					return 'canceled';
				}
				console.error(err);
				this._statusMessage = 'Installation could not start.';
				return 'error';
			} finally {
				this._installing = false;
			}
		}

		if (this.deferredPrompt) {
			this._installing = true;
			this._statusMessage = null;
			// Discard single-use event before awaiting prompt() / userChoice.
			const promptEvent = this.deferredPrompt;
			this.deferredPrompt = null;
			try {
				await promptEvent.prompt();
				const { outcome } = await promptEvent.userChoice;
				if (outcome === 'accepted') {
					this.installed = true;
					this._statusMessage = 'The app was installed.';
					return 'installed';
				}
				this._statusMessage = 'Installation canceled.';
				return 'canceled';
			} catch (err) {
				console.error(err);
				this._statusMessage = 'Installation could not start.';
				return 'error';
			} finally {
				this._installing = false;
			}
		}

		return 'unavailable';
	}

	dismiss(): void {
		this.dismissed = true;
		writeDismissed(true);
	}

	resetForTest(): void {
		this.deferredPrompt = null;
		this.supportsWebInstall = false;
		this.isStandalone = false;
		this.installed = false;
		this.dismissed = false;
		this._installing = false;
		this._statusMessage = null;
		writeDismissed(false);
	}
}

export const pwaInstall = new PwaInstallStore();
