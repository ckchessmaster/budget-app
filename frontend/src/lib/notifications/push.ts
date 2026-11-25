import { getAppConfig } from '$lib/config';
import { auth } from '$lib/stores/auth.svelte';

export interface PushServerConfig {
	enabled: boolean;
	vapid_public_key: string;
}

export interface PushSubscriptionPayload {
	endpoint: string;
	p256dh: string;
	auth: string;
}

export type PushSupportReason = 'supported' | 'ios_requires_install' | 'unsupported_browser';
export type PushPermissionState = NotificationPermission | 'unsupported';

export interface PushEnvironment {
	isIOS: boolean;
	isStandalone: boolean;
	isIOSStandalone: boolean;
	isSupported: boolean;
	supportReason: PushSupportReason;
	permission: PushPermissionState;
}

export type PushSubscribeResult =
	| { status: 'subscribed'; payload: PushSubscriptionPayload }
	| { status: 'denied' }
	| { status: 'dismissed' };

export const PUSH_PROMPT_DISMISSED_KEY = 'budget_push_prompt_dismissed';

interface NavigatorWithStandalone extends Navigator {
	standalone?: boolean;
}

async function authHeaders(): Promise<HeadersInit> {
	const headers: Record<string, string> = {
		'Content-Type': 'application/json'
	};
	const token = await auth.getValidToken();
	if (token) {
		headers['Authorization'] = `Bearer ${token}`;
	}
	return headers;
}

async function parseError(res: Response, fallback: string): Promise<string> {
	const body = (await res.json().catch(() => ({}))) as { error?: string };
	return body.error || `${fallback} (${res.status})`;
}

/**
 * Converts a URL-safe base64 VAPID public key into a Uint8Array backed by an ArrayBuffer
 * suitable for `PushManager.subscribe({ applicationServerKey })`.
 */
export function urlBase64ToUint8Array(base64String: string): Uint8Array<ArrayBuffer> {
	const trimmed = base64String.trim();
	const padding = '='.repeat((4 - (trimmed.length % 4)) % 4);
	const base64 = (trimmed + padding).replace(/-/g, '+').replace(/_/g, '/');
	const rawData = atob(base64);
	const buffer = new ArrayBuffer(rawData.length);
	const outputArray = new Uint8Array(buffer);
	for (let i = 0; i < rawData.length; i++) {
		outputArray[i] = rawData.charCodeAt(i);
	}
	return outputArray;
}

/**
 * Encodes an ArrayBuffer subscription key (`p256dh` or `auth`) into unpadded base64url.
 */
export function arrayBufferToBase64Url(buffer: ArrayBuffer | null): string {
	if (!buffer) return '';
	const bytes = new Uint8Array(buffer);
	let binary = '';
	for (let i = 0; i < bytes.length; i++) {
		binary += String.fromCharCode(bytes[i]);
	}
	return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '');
}

/**
 * Extracts `{ endpoint, p256dh, auth }` from a browser `PushSubscription`.
 */
export function extractSubscriptionPayload(subscription: PushSubscription): PushSubscriptionPayload {
	const endpoint = subscription.endpoint?.trim() ?? '';
	const jsonKeys = typeof subscription.toJSON === 'function' ? subscription.toJSON().keys : undefined;
	const p256dh =
		jsonKeys?.p256dh?.trim() ||
		arrayBufferToBase64Url(
			typeof subscription.getKey === 'function' ? subscription.getKey('p256dh') : null
		);
	const authKey =
		jsonKeys?.auth?.trim() ||
		arrayBufferToBase64Url(
			typeof subscription.getKey === 'function' ? subscription.getKey('auth') : null
		);

	if (!endpoint || !p256dh || !authKey) {
		throw new Error('Browser push subscription is missing endpoint or encryption keys.');
	}

	return {
		endpoint,
		p256dh,
		auth: authKey
	};
}

/**
 * Detects iOS / iPadOS devices (including iPadOS reporting MacIntel with touch points).
 */
export function isIOSDevice(nav: Navigator | undefined = typeof navigator !== 'undefined' ? navigator : undefined): boolean {
	if (!nav) return false;
	const ua = nav.userAgent ?? '';
	if (/iPad|iPhone|iPod/i.test(ua)) {
		return true;
	}
	return nav.platform === 'MacIntel' && (nav.maxTouchPoints ?? 0) > 1;
}

/**
 * Checks whether the web app is running in standalone/installed PWA display mode.
 */
export function isStandaloneMode(win: Window | undefined = typeof window !== 'undefined' ? window : undefined): boolean {
	if (!win) return false;
	const nav = win.navigator as NavigatorWithStandalone | undefined;
	if (nav && 'standalone' in nav && Boolean(nav.standalone)) {
		return true;
	}
	if (typeof win.matchMedia === 'function') {
		return win.matchMedia('(display-mode: standalone)').matches;
	}
	return false;
}

/**
 * Checks specifically for iOS Home Screen standalone PWA mode via `navigator.standalone`.
 */
export function isIOSStandaloneMode(win: Window | undefined = typeof window !== 'undefined' ? window : undefined): boolean {
	if (!win || !isIOSDevice(win.navigator)) return false;
	const nav = win.navigator as NavigatorWithStandalone;
	return 'standalone' in nav && Boolean(nav.standalone);
}

/**
 * Inspects browser capabilities, iOS standalone state, and current Notification permission.
 */
export function detectPushEnvironment(win: Window | undefined = typeof window !== 'undefined' ? window : undefined): PushEnvironment {
	if (!win) {
		return {
			isIOS: false,
			isStandalone: false,
			isIOSStandalone: false,
			isSupported: false,
			supportReason: 'unsupported_browser',
			permission: 'unsupported'
		};
	}

	const nav = win.navigator;
	const isIOS = isIOSDevice(nav);
	const isStandalone = isStandaloneMode(win);
	const isIOSStandalone = isIOSStandaloneMode(win);

	const notificationCtor = (win as Window & { Notification?: typeof Notification }).Notification;
	const permission: PushPermissionState = notificationCtor ? notificationCtor.permission : 'unsupported';

	if (isIOS && !isStandalone) {
		return {
			isIOS,
			isStandalone,
			isIOSStandalone,
			isSupported: false,
			supportReason: 'ios_requires_install',
			permission
		};
	}

	const hasServiceWorker = Boolean(nav && 'serviceWorker' in nav);
	const hasPushManager = 'PushManager' in win;
	const hasNotification = Boolean(notificationCtor && typeof notificationCtor.requestPermission === 'function');

	if (!hasServiceWorker || !hasPushManager || !hasNotification) {
		return {
			isIOS,
			isStandalone,
			isIOSStandalone,
			isSupported: false,
			supportReason: 'unsupported_browser',
			permission
		};
	}

	return {
		isIOS,
		isStandalone,
		isIOSStandalone,
		isSupported: true,
		supportReason: 'supported',
		permission
	};
}

export async function fetchPushConfig(): Promise<PushServerConfig> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/notifications/config`, {
		headers: await authHeaders()
	});
	if (!res.ok) {
		throw new Error(await parseError(res, 'Failed to load push notification config'));
	}
	return (await res.json()) as PushServerConfig;
}

export async function postPushSubscription(payload: PushSubscriptionPayload): Promise<void> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/notifications/subscribe`, {
		method: 'POST',
		headers: await authHeaders(),
		body: JSON.stringify(payload)
	});
	if (!res.ok) {
		throw new Error(await parseError(res, 'Failed to save push subscription'));
	}
}

export async function deletePushSubscription(endpoint: string): Promise<void> {
	const { apiBaseUrl } = getAppConfig();
	const res = await fetch(`${apiBaseUrl}/api/v1/notifications/subscribe`, {
		method: 'DELETE',
		headers: await authHeaders(),
		body: JSON.stringify({ endpoint })
	});
	if (!res.ok) {
		throw new Error(await parseError(res, 'Failed to remove push subscription'));
	}
}

/**
 * Returns the active PushSubscription without blocking on `navigator.serviceWorker.ready`.
 */
export async function getExistingPushSubscription(): Promise<PushSubscription | null> {
	if (typeof navigator === 'undefined' || !('serviceWorker' in navigator)) {
		return null;
	}
	const sw = navigator.serviceWorker;
	const registration = typeof sw.getRegistration === 'function' ? await sw.getRegistration() : undefined;
	if (!registration?.pushManager) {
		return null;
	}
	return registration.pushManager.getSubscription();
}

/**
 * Requests browser permission inside the active user gesture, subscribes via PushManager,
 * and persists the subscription to the backend API.
 *
 * `Notification.requestPermission()` MUST remain the first async call so iOS Safari/PWA
 * preserves transient user activation.
 */
export async function subscribeToPushNotifications(vapidPublicKey: string): Promise<PushSubscribeResult> {
	const trimmedKey = vapidPublicKey.trim();
	if (!trimmedKey) {
		throw new Error('Push notifications are not configured on this server.');
	}

	if (typeof window === 'undefined' || typeof Notification === 'undefined') {
		throw new Error('Web Push notifications are not supported in this browser.');
	}

	// Invoke requestPermission synchronously within the click handler before any other awaits.
	const permission = await Notification.requestPermission();
	if (permission === 'denied') {
		return { status: 'denied' };
	}
	if (permission !== 'granted') {
		return { status: 'dismissed' };
	}

	if (!('serviceWorker' in navigator)) {
		throw new Error('Service Worker support is required for push notifications.');
	}

	let registration =
		typeof navigator.serviceWorker.getRegistration === 'function'
			? await navigator.serviceWorker.getRegistration()
			: undefined;

	if (!registration) {
		registration = await navigator.serviceWorker.ready;
	}

	if (!registration?.pushManager) {
		throw new Error('PushManager is unavailable on the active service worker registration.');
	}

	let subscription = await registration.pushManager.getSubscription();
	if (!subscription) {
		subscription = await registration.pushManager.subscribe({
			userVisibleOnly: true,
			applicationServerKey: urlBase64ToUint8Array(trimmedKey)
		});
	}

	const payload = extractSubscriptionPayload(subscription);
	await postPushSubscription(payload);
	return { status: 'subscribed', payload };
}

/**
 * Removes the current device's push subscription from both the backend and browser PushManager.
 */
export async function unsubscribeFromPushNotifications(): Promise<boolean> {
	const subscription = await getExistingPushSubscription();
	if (!subscription) {
		return false;
	}

	await deletePushSubscription(subscription.endpoint);
	await subscription.unsubscribe();
	return true;
}

export function isPushPromptDismissed(): boolean {
	if (typeof window === 'undefined') return false;
	try {
		return window.localStorage.getItem(PUSH_PROMPT_DISMISSED_KEY) === 'true';
	} catch {
		return false;
	}
}

export function dismissPushPrompt(): void {
	if (typeof window === 'undefined') return;
	try {
		window.localStorage.setItem(PUSH_PROMPT_DISMISSED_KEY, 'true');
	} catch {
		// localStorage unavailable
	}
}

export function clearPushPromptDismissed(): void {
	if (typeof window === 'undefined') return;
	try {
		window.localStorage.removeItem(PUSH_PROMPT_DISMISSED_KEY);
	} catch {
		// localStorage unavailable
	}
}

/**
 * Determines whether the automatic iOS PWA onboarding modal should be displayed.
 * Desktop browsers opt in explicitly from Settings instead of an automatic modal.
 */
export function shouldShowPushOnboardingModal(
	env: PushEnvironment,
	serverEnabled: boolean,
	alreadySubscribed: boolean,
	dismissed: boolean
): boolean {
	if (!serverEnabled || alreadySubscribed || dismissed) {
		return false;
	}
	if (!env.isIOS) {
		return false;
	}
	if (env.isIOSStandalone) {
		return env.permission === 'default';
	}
	return !env.isStandalone;
}
