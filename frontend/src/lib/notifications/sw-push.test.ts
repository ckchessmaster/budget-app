import { describe, expect, it, vi } from 'vitest';
import {
	DEFAULT_PUSH_BODY,
	DEFAULT_PUSH_TITLE,
	DEFAULT_PUSH_URL,
	handleNotificationClick,
	handlePushEvent,
	parsePushPayload,
	sanitizeNotificationUrl,
	type ClientsLike,
	type NotificationRegistrationLike
} from './sw-push';

describe('sw-push', () => {
	describe('parsePushPayload', () => {
		it('parses valid JSON notification payloads', () => {
			expect(
				parsePushPayload(
					JSON.stringify({
						title: 'Spending Alert: Dining',
						body: '85% spent this month',
						url: '/budget'
					})
				)
			).toEqual({
				title: 'Spending Alert: Dining',
				body: '85% spent this month',
				url: '/budget'
			});
		});

		it('fills missing fields with defaults for partial JSON payloads', () => {
			expect(parsePushPayload(JSON.stringify({ body: 'Statement due tomorrow' }))).toEqual({
				title: DEFAULT_PUSH_TITLE,
				body: 'Statement due tomorrow',
				url: DEFAULT_PUSH_URL
			});
		});

		it('treats non-JSON text as notification body and falls back for empty/null', () => {
			expect(parsePushPayload('Sync complete')).toEqual({
				title: DEFAULT_PUSH_TITLE,
				body: 'Sync complete',
				url: DEFAULT_PUSH_URL
			});
			expect(parsePushPayload(null)).toEqual({
				title: DEFAULT_PUSH_TITLE,
				body: DEFAULT_PUSH_BODY,
				url: DEFAULT_PUSH_URL
			});
			expect(parsePushPayload('   ')).toEqual({
				title: DEFAULT_PUSH_TITLE,
				body: DEFAULT_PUSH_BODY,
				url: DEFAULT_PUSH_URL
			});
		});
	});

	describe('sanitizeNotificationUrl', () => {
		const origin = 'https://budget.example.com';

		it('accepts relative paths and same-origin URLs', () => {
			expect(sanitizeNotificationUrl('/accounts', origin)).toBe(
				'https://budget.example.com/accounts'
			);
			expect(sanitizeNotificationUrl('https://budget.example.com/budget?m=2026-11', origin)).toBe(
				'https://budget.example.com/budget?m=2026-11'
			);
		});

		it('rejects cross-origin, protocol-relative, and non-HTTP(S) URLs', () => {
			const fallback = 'https://budget.example.com/budget';
			expect(sanitizeNotificationUrl('https://evil.test/phish', origin)).toBe(fallback);
			expect(sanitizeNotificationUrl('//evil.test/phish', origin)).toBe(fallback);
			expect(sanitizeNotificationUrl('javascript:alert(1)', origin)).toBe(fallback);
			expect(sanitizeNotificationUrl('', origin)).toBe(fallback);
			expect(sanitizeNotificationUrl(undefined, origin)).toBe(fallback);
		});
	});

	describe('handlePushEvent', () => {
		it('parses push event payload and calls registration.showNotification inside waitUntil', async () => {
			let pending: Promise<unknown> | undefined;
			const showNotification = vi.fn().mockResolvedValue(undefined);
			const registration: NotificationRegistrationLike = { showNotification };

			handlePushEvent(
				{
					data: {
						text: () =>
							JSON.stringify({
								title: 'Statement Due Soon: Sapphire',
								body: 'Statement ($450.00) is due in 2 days.',
								url: '/accounts'
							})
					},
					waitUntil: (promise) => {
						pending = promise;
					}
				},
				registration
			);

			expect(pending).toBeDefined();
			await pending;

			expect(showNotification).toHaveBeenCalledTimes(1);
			expect(showNotification).toHaveBeenCalledWith('Statement Due Soon: Sapphire', {
				body: 'Statement ($450.00) is due in 2 days.',
				icon: '/favicon.png',
				badge: '/favicon.png',
				data: { url: '/accounts' }
			});
		});
	});

	describe('handleNotificationClick', () => {
		const origin = 'https://budget.example.com';

		it('closes notification, navigates and focuses an existing same-origin client when URL differs', async () => {
			let pending: Promise<unknown> | undefined;
			const close = vi.fn();
			const focus = vi.fn().mockResolvedValue(undefined);
			const navigate = vi.fn().mockResolvedValue(undefined);
			const openWindow = vi.fn().mockResolvedValue(undefined);

			const clients: ClientsLike = {
				matchAll: vi.fn().mockResolvedValue([
					{
						url: 'https://budget.example.com/budget',
						focus,
						navigate
					}
				]),
				openWindow
			};

			handleNotificationClick(
				{
					notification: {
						data: { url: '/accounts' },
						close
					},
					waitUntil: (promise) => {
						pending = promise;
					}
				},
				clients,
				origin
			);

			expect(close).toHaveBeenCalledTimes(1);
			await pending;

			expect(navigate).toHaveBeenCalledWith('https://budget.example.com/accounts');
			expect(focus).toHaveBeenCalledTimes(1);
			expect(openWindow).not.toHaveBeenCalled();
		});

		it('focuses matching client without calling navigate when already on target URL', async () => {
			let pending: Promise<unknown> | undefined;
			const focus = vi.fn().mockResolvedValue(undefined);
			const navigate = vi.fn().mockResolvedValue(undefined);

			const clients: ClientsLike = {
				matchAll: vi.fn().mockResolvedValue([
					{
						url: 'https://budget.example.com/accounts',
						focus,
						navigate
					}
				]),
				openWindow: vi.fn()
			};

			handleNotificationClick(
				{
					notification: {
						data: { url: '/accounts' },
						close: vi.fn()
					},
					waitUntil: (promise) => {
						pending = promise;
					}
				},
				clients,
				origin
			);

			await pending;

			expect(navigate).not.toHaveBeenCalled();
			expect(focus).toHaveBeenCalledTimes(1);
		});

		it('opens a new window with sanitized URL when no same-origin window is open', async () => {
			let pending: Promise<unknown> | undefined;
			const openWindow = vi.fn().mockResolvedValue(undefined);

			const clients: ClientsLike = {
				matchAll: vi.fn().mockResolvedValue([]),
				openWindow
			};

			handleNotificationClick(
				{
					notification: {
						data: { url: 'https://evil.test/steal' },
						close: vi.fn()
					},
					waitUntil: (promise) => {
						pending = promise;
					}
				},
				clients,
				origin
			);

			await pending;

			expect(openWindow).toHaveBeenCalledWith('https://budget.example.com/budget');
		});
	});
});
