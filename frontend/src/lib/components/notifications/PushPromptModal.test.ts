import { fireEvent, render, waitFor } from '@testing-library/svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { PushEnvironment } from '$lib/notifications/push';
import PushPromptModal from './PushPromptModal.svelte';

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

const SAMPLE_VAPID_PUBLIC_KEY =
	'BEl62iUYgUivxIkv69yViEuiBIa-Ib9-SkvMeAtA3LFgDzkrxZJjSgSnfckjBJuBkr3qBUYIHBQFLXYp5Nksh8';

const iosStandaloneEnv: PushEnvironment = {
	isIOS: true,
	isStandalone: true,
	isIOSStandalone: true,
	isSupported: true,
	supportReason: 'supported',
	permission: 'default'
};

const iosSafariBrowserEnv: PushEnvironment = {
	isIOS: true,
	isStandalone: false,
	isIOSStandalone: false,
	isSupported: false,
	supportReason: 'ios_requires_install',
	permission: 'unsupported'
};

describe('PushPromptModal', () => {
	const originalNavigatorDescriptor = Object.getOwnPropertyDescriptor(window, 'navigator');
	const originalNotificationDescriptor = Object.getOwnPropertyDescriptor(window, 'Notification');
	const originalPushManagerDescriptor = Object.getOwnPropertyDescriptor(window, 'PushManager');
	const originalFetch = globalThis.fetch;

	let requestPermissionMock: ReturnType<typeof vi.fn<() => Promise<NotificationPermission>>>;
	let subscribeMock: ReturnType<typeof vi.fn>;
	let fetchMock: ReturnType<typeof vi.fn>;

	beforeEach(() => {
		requestPermissionMock = vi.fn<() => Promise<NotificationPermission>>().mockResolvedValue('granted');
		subscribeMock = vi.fn().mockResolvedValue({
			endpoint: 'https://web.push.apple.com/device-1',
			toJSON: () => ({
				endpoint: 'https://web.push.apple.com/device-1',
				keys: {
					p256dh: 'p256dh-val',
					auth: 'auth-val'
				}
			})
		});
		fetchMock = vi
			.fn()
			.mockResolvedValue(new Response(JSON.stringify({ status: 'subscribed' }), { status: 201 }));
		globalThis.fetch = fetchMock as unknown as typeof fetch;

		Object.defineProperty(window, 'Notification', {
			value: {
				permission: 'default',
				requestPermission: requestPermissionMock
			},
			configurable: true
		});
		Object.defineProperty(window, 'PushManager', {
			value: function PushManager() {},
			configurable: true
		});
		Object.defineProperty(window, 'navigator', {
			value: {
				userAgent: 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X)',
				platform: 'iPhone',
				maxTouchPoints: 5,
				standalone: true,
				serviceWorker: {
					getRegistration: vi.fn().mockResolvedValue({
						pushManager: {
							getSubscription: vi.fn().mockResolvedValue(null),
							subscribe: subscribeMock
						}
					}),
					ready: Promise.resolve({})
				}
			},
			configurable: true
		});
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
		globalThis.fetch = originalFetch;
		vi.restoreAllMocks();
	});

	it('binds Notification.requestPermission strictly to the Enable Alerts click gesture', async () => {
		const onSubscribed = vi.fn();
		const { getByTestId } = render(PushPromptModal, {
			props: {
				open: true,
				vapidPublicKey: SAMPLE_VAPID_PUBLIC_KEY,
				serverEnabled: true,
				environment: iosStandaloneEnv,
				onClose: vi.fn(),
				onSubscribed
			}
		});

		// Permission prompt MUST NOT fire on mount.
		expect(requestPermissionMock).not.toHaveBeenCalled();
		expect(subscribeMock).not.toHaveBeenCalled();
		expect(fetchMock).not.toHaveBeenCalled();

		await fireEvent.click(getByTestId('enable-alerts-btn'));

		await waitFor(() => {
			expect(requestPermissionMock).toHaveBeenCalledTimes(1);
			expect(subscribeMock).toHaveBeenCalledTimes(1);
			expect(fetchMock).toHaveBeenCalledWith(
				'http://localhost:8080/api/v1/notifications/subscribe',
				expect.objectContaining({
					method: 'POST',
					body: JSON.stringify({
						endpoint: 'https://web.push.apple.com/device-1',
						p256dh: 'p256dh-val',
						auth: 'auth-val'
					})
				})
			);
			expect(onSubscribed).toHaveBeenCalledWith({
				endpoint: 'https://web.push.apple.com/device-1',
				p256dh: 'p256dh-val',
				auth: 'auth-val'
			});
			expect(getByTestId('push-subscribed-success')).toBeTruthy();
		});
	});

	it('renders iOS Home Screen installation instructions when not in standalone mode', () => {
		const { getByTestId, queryByTestId } = render(PushPromptModal, {
			props: {
				open: true,
				vapidPublicKey: SAMPLE_VAPID_PUBLIC_KEY,
				serverEnabled: true,
				environment: iosSafariBrowserEnv,
				onClose: vi.fn()
			}
		});

		expect(getByTestId('ios-install-guide')).toBeTruthy();
		expect(queryByTestId('enable-alerts-btn')).toBeNull();
		expect(requestPermissionMock).not.toHaveBeenCalled();
	});

	it('shows blocked alert message and skips subscribing when user denies permission', async () => {
		requestPermissionMock.mockResolvedValue('denied');

		const { getByTestId, getByRole } = render(PushPromptModal, {
			props: {
				open: true,
				vapidPublicKey: SAMPLE_VAPID_PUBLIC_KEY,
				serverEnabled: true,
				environment: iosStandaloneEnv,
				onClose: vi.fn()
			}
		});

		await fireEvent.click(getByTestId('enable-alerts-btn'));

		await waitFor(() => {
			expect(getByRole('alert').textContent).toContain('blocked');
		});
		expect(subscribeMock).not.toHaveBeenCalled();
		expect(fetchMock).not.toHaveBeenCalled();
	});

	it('renders server-disabled state without Enable Alerts button', () => {
		const { getByText, queryByTestId } = render(PushPromptModal, {
			props: {
				open: true,
				vapidPublicKey: '',
				serverEnabled: false,
				environment: iosStandaloneEnv,
				onClose: vi.fn()
			}
		});

		expect(getByText('Push notifications are unavailable')).toBeTruthy();
		expect(queryByTestId('enable-alerts-btn')).toBeNull();
		expect(requestPermissionMock).not.toHaveBeenCalled();
	});

	it('dismisses on Not Now and Escape without requesting permission', async () => {
		const onClose = vi.fn();
		const onDismiss = vi.fn();
		const { getByTestId } = render(PushPromptModal, {
			props: {
				open: true,
				vapidPublicKey: SAMPLE_VAPID_PUBLIC_KEY,
				serverEnabled: true,
				environment: iosStandaloneEnv,
				onClose,
				onDismiss
			}
		});

		await fireEvent.click(getByTestId('push-not-now-btn'));
		expect(onDismiss).toHaveBeenCalledTimes(1);
		expect(onClose).toHaveBeenCalledTimes(1);
		expect(requestPermissionMock).not.toHaveBeenCalled();

		await fireEvent.keyDown(window, { key: 'Escape' });
		expect(onClose).toHaveBeenCalledTimes(2);
		expect(requestPermissionMock).not.toHaveBeenCalled();
	});

	it('displays an alert error and keeps modal open if subscribing fails', async () => {
		fetchMock.mockResolvedValue(
			new Response(JSON.stringify({ error: 'push_notifications_disabled' }), { status: 503 })
		);
		const onClose = vi.fn();
		const { getByTestId, getByRole } = render(PushPromptModal, {
			props: {
				open: true,
				vapidPublicKey: SAMPLE_VAPID_PUBLIC_KEY,
				serverEnabled: true,
				environment: iosStandaloneEnv,
				onClose
			}
		});

		await fireEvent.click(getByTestId('enable-alerts-btn'));

		await waitFor(() => {
			expect(getByRole('alert').textContent).toContain('push_notifications_disabled');
		});
		expect(onClose).not.toHaveBeenCalled();
	});
});
