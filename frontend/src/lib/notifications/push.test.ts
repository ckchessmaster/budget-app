import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
	arrayBufferToBase64Url,
	clearPushPromptDismissed,
	detectPushEnvironment,
	dismissPushPrompt,
	isPushPromptDismissed,
	shouldShowPushOnboardingModal,
	subscribeToPushNotifications,
	unsubscribeFromPushNotifications,
	urlBase64ToUint8Array,
	type PushEnvironment
} from './push';

const { getValidTokenMock, getAppConfigMock } = vi.hoisted(() => ({
	getValidTokenMock: vi.fn().mockResolvedValue('test-jwt-token'),
	getAppConfigMock: vi.fn().mockReturnValue({
		oidcAuthority: 'http://localhost:5556/dex',
		oidcClientId: 'budget-app-client',
		apiBaseUrl: 'http://localhost:8080'
	})
}));

vi.mock('$lib/stores/auth.svelte', () => ({
	auth: {
		getValidToken: getValidTokenMock
	}
}));

vi.mock('$lib/config', () => ({
	getAppConfig: getAppConfigMock
}));

describe('push notification client', () => {
	const originalNavigatorDescriptor = Object.getOwnPropertyDescriptor(window, 'navigator');
	const originalNotificationDescriptor = Object.getOwnPropertyDescriptor(window, 'Notification');
	const originalPushManagerDescriptor = Object.getOwnPropertyDescriptor(window, 'PushManager');
	const originalMatchMediaDescriptor = Object.getOwnPropertyDescriptor(window, 'matchMedia');
	const originalFetch = globalThis.fetch;

	beforeEach(() => {
		clearPushPromptDismissed();
		getValidTokenMock.mockClear();
	});

	afterEach(() => {
		if (originalNavigatorDescriptor) {
			Object.defineProperty(window, 'navigator', originalNavigatorDescriptor);
		}
		if (originalNotificationDescriptor) {
			Object.defineProperty(window, 'Notification', originalNotificationDescriptor);
		} else {
			delete (window as unknown as Record<string, unknown>).Notification;
		}
		if (originalPushManagerDescriptor) {
			Object.defineProperty(window, 'PushManager', originalPushManagerDescriptor);
		} else {
			delete (window as unknown as Record<string, unknown>).PushManager;
		}
		if (originalMatchMediaDescriptor) {
			Object.defineProperty(window, 'matchMedia', originalMatchMediaDescriptor);
		}
		globalThis.fetch = originalFetch;
		vi.restoreAllMocks();
	});

	function stubBrowserEnvironment(opts: {
		userAgent?: string;
		platform?: string;
		maxTouchPoints?: number;
		standalone?: boolean;
		displayModeStandalone?: boolean;
		hasServiceWorker?: boolean;
		hasPushManager?: boolean;
		notificationPermission?: NotificationPermission;
		requestPermission?: () => Promise<NotificationPermission>;
		registration?: {
			pushManager?: {
				getSubscription: () => Promise<PushSubscription | null>;
				subscribe: (options: PushSubscriptionOptionsInit) => Promise<PushSubscription>;
			};
		};
	}) {
		const navMock: Record<string, unknown> = {
			userAgent: opts.userAgent ?? 'Mozilla/5.0 (X11; Linux x86_64)',
			platform: opts.platform ?? 'Linux x86_64',
			maxTouchPoints: opts.maxTouchPoints ?? 0
		};
		if (opts.standalone !== undefined) {
			navMock.standalone = opts.standalone;
		}
		if (opts.hasServiceWorker ?? true) {
			navMock.serviceWorker = {
				getRegistration: vi.fn().mockResolvedValue(opts.registration),
				ready: Promise.resolve(opts.registration)
			};
		}

		Object.defineProperty(window, 'navigator', {
			value: navMock,
			configurable: true
		});

		if (opts.hasPushManager ?? true) {
			Object.defineProperty(window, 'PushManager', {
				value: function PushManager() {},
				configurable: true
			});
		} else {
			delete (window as unknown as Record<string, unknown>).PushManager;
		}

		if (opts.notificationPermission !== undefined || opts.requestPermission) {
			const requestPermission =
				opts.requestPermission ?? vi.fn().mockResolvedValue(opts.notificationPermission ?? 'default');
			Object.defineProperty(window, 'Notification', {
				value: {
					permission: opts.notificationPermission ?? 'default',
					requestPermission
				},
				configurable: true
			});
		} else {
			delete (window as unknown as Record<string, unknown>).Notification;
		}

		Object.defineProperty(window, 'matchMedia', {
			value: vi.fn().mockImplementation((query: string) => ({
				matches: query === '(display-mode: standalone)' ? Boolean(opts.displayModeStandalone) : false,
				media: query,
				addEventListener: vi.fn(),
				removeEventListener: vi.fn()
			})),
			configurable: true
		});
	}

	it('converts base64url VAPID public keys and round-trips ArrayBuffers', () => {
		// 65-byte uncompressed P-256 point encoded as 87-char unpadded base64url (includes '-' and '_').
		const p256Key =
			'BAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8gISIjJCUmJygpKissLS4vMDEyMzQ1Njc4OTo7PD0-P_A';
		const bytes = urlBase64ToUint8Array(p256Key);
		expect(bytes).toBeInstanceOf(Uint8Array);
		expect(bytes.buffer).toBeInstanceOf(ArrayBuffer);
		expect(bytes.length).toBe(65);
		expect(arrayBufferToBase64Url(bytes.buffer)).toBe(p256Key);
	});

	it('detects iOS Safari browser as requiring Home Screen install, and standalone as supported', () => {
		stubBrowserEnvironment({
			userAgent: 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15',
			platform: 'iPhone',
			standalone: false,
			hasPushManager: false
		});
		const safariEnv = detectPushEnvironment(window);
		expect(safariEnv).toMatchObject({
			isIOS: true,
			isStandalone: false,
			isIOSStandalone: false,
			isSupported: false,
			supportReason: 'ios_requires_install'
		});

		stubBrowserEnvironment({
			userAgent: 'Mozilla/5.0 (iPad; CPU OS 17_4 like Mac OS X) AppleWebKit/605.1.15',
			platform: 'MacIntel',
			maxTouchPoints: 5,
			standalone: true,
			hasPushManager: true,
			notificationPermission: 'default'
		});
		const standaloneEnv = detectPushEnvironment(window);
		expect(standaloneEnv).toMatchObject({
			isIOS: true,
			isStandalone: true,
			isIOSStandalone: true,
			isSupported: true,
			supportReason: 'supported',
			permission: 'default'
		});
	});

	it('detects unsupported browsers cleanly', () => {
		stubBrowserEnvironment({
			hasServiceWorker: false,
			hasPushManager: false
		});
		expect(detectPushEnvironment(window)).toMatchObject({
			isSupported: false,
			supportReason: 'unsupported_browser',
			permission: 'unsupported'
		});
	});

	it('shows onboarding modal by default for supported browsers and iOS Safari until enabled, denied, or dismissed', () => {
		const iosStandaloneDefault: PushEnvironment = {
			isIOS: true,
			isStandalone: true,
			isIOSStandalone: true,
			isSupported: true,
			supportReason: 'supported',
			permission: 'default'
		};
		const iosSafari: PushEnvironment = {
			isIOS: true,
			isStandalone: false,
			isIOSStandalone: false,
			isSupported: false,
			supportReason: 'ios_requires_install',
			permission: 'unsupported'
		};
		const desktop: PushEnvironment = {
			isIOS: false,
			isStandalone: false,
			isIOSStandalone: false,
			isSupported: true,
			supportReason: 'supported',
			permission: 'default'
		};
		const unsupported: PushEnvironment = {
			isIOS: false,
			isStandalone: false,
			isIOSStandalone: false,
			isSupported: false,
			supportReason: 'unsupported_browser',
			permission: 'unsupported'
		};

		expect(shouldShowPushOnboardingModal(iosStandaloneDefault, true, false, false)).toBe(true);
		expect(shouldShowPushOnboardingModal(iosSafari, true, false, false)).toBe(true);
		expect(shouldShowPushOnboardingModal(desktop, true, false, false)).toBe(true);
		expect(shouldShowPushOnboardingModal(unsupported, true, false, false)).toBe(false);
		expect(shouldShowPushOnboardingModal(desktop, true, false, false)).toBe(true);
		expect(shouldShowPushOnboardingModal(iosStandaloneDefault, false, false, false)).toBe(false);
		expect(shouldShowPushOnboardingModal(iosStandaloneDefault, true, true, false)).toBe(false);
		expect(shouldShowPushOnboardingModal(iosStandaloneDefault, true, false, true)).toBe(false);
		expect(
			shouldShowPushOnboardingModal({ ...iosStandaloneDefault, permission: 'denied' }, true, false, false)
		).toBe(false);
		expect(
			shouldShowPushOnboardingModal({ ...desktop, permission: 'denied' }, true, false, false)
		).toBe(false);

		expect(isPushPromptDismissed()).toBe(false);
		dismissPushPrompt();
		expect(isPushPromptDismissed()).toBe(true);
	});

	it('requests permission first, subscribes via PushManager, and POSTs keys to backend', async () => {
		const callOrder: string[] = [];
		const mockSubscription = {
			endpoint: 'https://web.push.apple.com/Q123',
			toJSON: () => ({
				endpoint: 'https://web.push.apple.com/Q123',
				keys: {
					p256dh: 'p256dh-test-key',
					auth: 'auth-test-key'
				}
			}),
			getKey: vi.fn(),
			unsubscribe: vi.fn()
		} as unknown as PushSubscription;

		const requestPermission = vi.fn().mockImplementation(async () => {
			callOrder.push('requestPermission');
			return 'granted';
		});
		const subscribe = vi.fn().mockImplementation(async () => {
			callOrder.push('pushManager.subscribe');
			return mockSubscription;
		});
		const fetchMock = vi.fn().mockImplementation(async () => {
			callOrder.push('fetch.subscribe');
			return new Response(JSON.stringify({ status: 'subscribed' }), { status: 201 });
		});
		globalThis.fetch = fetchMock as unknown as typeof fetch;

		stubBrowserEnvironment({
			standalone: true,
			userAgent: 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X)',
			notificationPermission: 'default',
			requestPermission,
			registration: {
				pushManager: {
					getSubscription: vi.fn().mockResolvedValue(null),
					subscribe
				}
			}
		});

		const result = await subscribeToPushNotifications(
			'BEl62iUYgUivxIkv69yViEuiBIa-Ib9-SkvMeAtA3LFgDzkrxZJjSgSnfckjBJuBkr3qBUYIHBQFLXYp5Nksh8'
		);

		expect(result).toEqual({
			status: 'subscribed',
			payload: {
				endpoint: 'https://web.push.apple.com/Q123',
				p256dh: 'p256dh-test-key',
				auth: 'auth-test-key'
			}
		});
		expect(callOrder).toEqual(['requestPermission', 'pushManager.subscribe', 'fetch.subscribe']);
		expect(fetchMock).toHaveBeenCalledWith(
			'http://localhost:8080/api/v1/notifications/subscribe',
			expect.objectContaining({
				method: 'POST',
				headers: expect.objectContaining({
					Authorization: 'Bearer test-jwt-token',
					'Content-Type': 'application/json'
				}),
				body: JSON.stringify({
					endpoint: 'https://web.push.apple.com/Q123',
					p256dh: 'p256dh-test-key',
					auth: 'auth-test-key'
				})
			})
		);
	});

	it('stops before PushManager subscribe when permission is denied', async () => {
		const subscribe = vi.fn();
		const fetchMock = vi.fn();
		globalThis.fetch = fetchMock as unknown as typeof fetch;

		stubBrowserEnvironment({
			requestPermission: vi.fn().mockResolvedValue('denied'),
			registration: {
				pushManager: {
					getSubscription: vi.fn(),
					subscribe
				}
			}
		});

		const result = await subscribeToPushNotifications('BEl62iUYgUivxIkv69yViEuiBIa-Ib9');
		expect(result).toEqual({ status: 'denied' });
		expect(subscribe).not.toHaveBeenCalled();
		expect(fetchMock).not.toHaveBeenCalled();
	});

	it('deletes subscription from backend and unsubscribes in browser', async () => {
		const unsubscribe = vi.fn().mockResolvedValue(true);
		const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 204 }));
		globalThis.fetch = fetchMock as unknown as typeof fetch;

		stubBrowserEnvironment({
			registration: {
				pushManager: {
					getSubscription: vi.fn().mockResolvedValue({
						endpoint: 'https://push.example.com/sub-1',
						unsubscribe
					} as unknown as PushSubscription),
					subscribe: vi.fn()
				}
			}
		});

		const removed = await unsubscribeFromPushNotifications();
		expect(removed).toBe(true);
		expect(fetchMock).toHaveBeenCalledWith(
			'http://localhost:8080/api/v1/notifications/subscribe',
			expect.objectContaining({
				method: 'DELETE',
				body: JSON.stringify({ endpoint: 'https://push.example.com/sub-1' })
			})
		);
		expect(unsubscribe).toHaveBeenCalledTimes(1);
	});
});
