import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
	PWA_INSTALL_DISMISSED_KEY,
	pwaInstall,
	type BeforeInstallPromptEvent
} from './pwaInstall.svelte';

function createBeforeInstallEvent(outcome: 'accepted' | 'dismissed' = 'accepted') {
	let resolveChoice!: (val: { outcome: 'accepted' | 'dismissed'; platform: string }) => void;
	const userChoice = new Promise<{ outcome: 'accepted' | 'dismissed'; platform: string }>((res) => {
		resolveChoice = res;
	});
	const prompt = vi.fn().mockImplementation(async () => {
		resolveChoice({ outcome, platform: 'web' });
	});
	const event = new Event('beforeinstallprompt', { cancelable: true }) as BeforeInstallPromptEvent;
	Object.defineProperty(event, 'prompt', { value: prompt });
	Object.defineProperty(event, 'userChoice', { value: userChoice });
	return { event, prompt };
}

describe('pwaInstall store', () => {
	let cleanup: (() => void) | null = null;

	beforeEach(() => {
		cleanup?.();
		cleanup = null;
		pwaInstall.resetForTest();
		vi.restoreAllMocks();
		delete (Navigator.prototype as unknown as Record<string, unknown>).install;
		Object.defineProperty(window, 'matchMedia', {
			writable: true,
			configurable: true,
			value: vi.fn().mockReturnValue({ matches: false })
		});
		Object.defineProperty(window.navigator, 'standalone', {
			writable: true,
			configurable: true,
			value: false
		});
	});

	it('captures beforeinstallprompt, prevents default, and consumes single-use event on install', async () => {
		cleanup = pwaInstall.init();
		expect(pwaInstall.canInstall).toBe(false);
		expect(pwaInstall.shouldShowBanner).toBe(false);

		const { event, prompt } = createBeforeInstallEvent('accepted');
		window.dispatchEvent(event);

		expect(event.defaultPrevented).toBe(true);
		expect(pwaInstall.canInstall).toBe(true);
		expect(pwaInstall.shouldShowBanner).toBe(true);

		const outcome = await pwaInstall.promptInstall();
		expect(outcome).toBe('installed');
		expect(prompt).toHaveBeenCalledTimes(1);
		expect(pwaInstall.canInstall).toBe(false);
		expect(pwaInstall.shouldShowBanner).toBe(false);
	});

	it('hides install affordance after a dismissed beforeinstallprompt until a new event fires', async () => {
		cleanup = pwaInstall.init();
		const first = createBeforeInstallEvent('dismissed');
		window.dispatchEvent(first.event);
		expect(pwaInstall.canInstall).toBe(true);

		const outcome = await pwaInstall.promptInstall();
		expect(outcome).toBe('canceled');
		expect(pwaInstall.canInstall).toBe(false);

		const second = createBeforeInstallEvent('accepted');
		window.dispatchEvent(second.event);
		expect(pwaInstall.canInstall).toBe(true);
	});

	it('prefers navigator.install() when supported and keeps button available on AbortError', async () => {
		const abortErr = Object.assign(new Error('User canceled'), { name: 'AbortError' });
		const installMock = vi.fn().mockRejectedValueOnce(abortErr).mockResolvedValueOnce(undefined);
		Object.defineProperty(Navigator.prototype, 'install', {
			configurable: true,
			writable: true,
			value: installMock
		});

		cleanup = pwaInstall.init();
		expect(pwaInstall.canInstall).toBe(true);

		const firstOutcome = await pwaInstall.promptInstall();
		expect(firstOutcome).toBe('canceled');
		expect(pwaInstall.canInstall).toBe(true);

		const secondOutcome = await pwaInstall.promptInstall();
		expect(secondOutcome).toBe('installed');
		expect(installMock).toHaveBeenCalledTimes(2);
		expect(pwaInstall.canInstall).toBe(false);
	});

	it('stays hidden in standalone mode and persists banner dismissal', () => {
		Object.defineProperty(window, 'matchMedia', {
			writable: true,
			configurable: true,
			value: vi.fn().mockReturnValue({ matches: true })
		});

		cleanup = pwaInstall.init();
		const { event } = createBeforeInstallEvent('accepted');
		window.dispatchEvent(event);
		expect(pwaInstall.canInstall).toBe(false);
		cleanup();

		Object.defineProperty(window, 'matchMedia', {
			writable: true,
			configurable: true,
			value: vi.fn().mockReturnValue({ matches: false })
		});
		cleanup = pwaInstall.init();
		window.dispatchEvent(createBeforeInstallEvent('accepted').event);
		expect(pwaInstall.shouldShowBanner).toBe(true);

		pwaInstall.dismiss();
		expect(pwaInstall.shouldShowBanner).toBe(false);
		expect(pwaInstall.canInstall).toBe(true);
		expect(localStorage.getItem(PWA_INSTALL_DISMISSED_KEY)).toBe('true');
	});

	it('marks app as installed when appinstalled event fires', () => {
		cleanup = pwaInstall.init();
		window.dispatchEvent(createBeforeInstallEvent('accepted').event);
		expect(pwaInstall.canInstall).toBe(true);

		window.dispatchEvent(new Event('appinstalled'));
		expect(pwaInstall.canInstall).toBe(false);
		expect(pwaInstall.statusMessage).toBe('Budget App was installed.');
	});
});
