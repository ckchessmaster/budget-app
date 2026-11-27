export const DEFAULT_PUSH_TITLE = 'Budget App';
export const DEFAULT_PUSH_BODY = 'You have a new budget alert.';
export const DEFAULT_PUSH_URL = '/budget';

export interface PushPayload {
	title: string;
	body: string;
	url: string;
	tag?: string;
}

export interface PushDataLike {
	text?: () => string;
	json?: () => unknown;
}

export interface PushEventLike {
	data?: PushDataLike | null;
	waitUntil(promise: Promise<unknown>): void;
}

export interface NotificationRegistrationLike {
	showNotification(
		title: string,
		options?: {
			body?: string;
			icon?: string;
			badge?: string;
			tag?: string;
			data?: { url: string };
		}
	): Promise<void>;
}

export interface NotificationLike {
	data?: unknown;
	close(): void;
}

export interface NotificationEventLike {
	notification: NotificationLike;
	waitUntil(promise: Promise<unknown>): void;
}

export interface WindowClientLike {
	url: string;
	focus?: () => Promise<unknown>;
	navigate?: (url: string) => Promise<unknown>;
}

export interface ClientsLike {
	matchAll(options?: {
		type?: 'window';
		includeUncontrolled?: boolean;
	}): Promise<readonly WindowClientLike[]>;
	openWindow(url: string): Promise<unknown>;
}

/**
 * Resolves a notification click destination to a safe same-origin URL.
 * Rejects protocol-relative paths, cross-origin URLs, and non-HTTP(S) schemes.
 */
export function sanitizeNotificationUrl(rawUrl: unknown, origin: string): string {
	const fallback = new URL(DEFAULT_PUSH_URL, origin).href;
	if (typeof rawUrl !== 'string') {
		return fallback;
	}

	const trimmed = rawUrl.trim();
	if (!trimmed) {
		return fallback;
	}

	// Reject protocol-relative URLs such as "//evil.test".
	if (trimmed.startsWith('//')) {
		return fallback;
	}

	if (trimmed.startsWith('/')) {
		try {
			const resolved = new URL(trimmed, origin);
			return resolved.origin === origin ? resolved.href : fallback;
		} catch {
			return fallback;
		}
	}

	try {
		const resolved = new URL(trimmed);
		if (
			resolved.origin === origin &&
			(resolved.protocol === 'https:' || resolved.protocol === 'http:')
		) {
			return resolved.href;
		}
	} catch {
		// Invalid URL syntax falls back to the default route.
	}

	return fallback;
}

/**
 * Parses a Web Push payload string into normalized notification fields.
 * Supports JSON `{ title, body, url }` as well as plain-text payloads.
 */
export function parsePushPayload(raw: string | null | undefined): PushPayload {
	if (typeof raw !== 'string') {
		return {
			title: DEFAULT_PUSH_TITLE,
			body: DEFAULT_PUSH_BODY,
			url: DEFAULT_PUSH_URL
		};
	}

	const trimmed = raw.trim();
	if (!trimmed) {
		return {
			title: DEFAULT_PUSH_TITLE,
			body: DEFAULT_PUSH_BODY,
			url: DEFAULT_PUSH_URL
		};
	}

	try {
		const parsed = JSON.parse(trimmed) as Record<string, unknown>;
		if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
			const title =
				typeof parsed.title === 'string' && parsed.title.trim()
					? parsed.title.trim()
					: DEFAULT_PUSH_TITLE;
			const body =
				typeof parsed.body === 'string' && parsed.body.trim()
					? parsed.body.trim()
					: DEFAULT_PUSH_BODY;
			const url =
				typeof parsed.url === 'string' && parsed.url.trim()
					? parsed.url.trim()
					: DEFAULT_PUSH_URL;
			const tag =
				typeof parsed.tag === 'string' && parsed.tag.trim() ? parsed.tag.trim() : undefined;
			return tag ? { title, body, url, tag } : { title, body, url };
		}
	} catch {
		// Fall through to plain-text payload handling.
	}

	return {
		title: DEFAULT_PUSH_TITLE,
		body: trimmed,
		url: DEFAULT_PUSH_URL
	};
}

function readPushRawText(data: PushDataLike | null | undefined): string | null {
	if (!data) return null;

	if (typeof data.text === 'function') {
		try {
			return data.text();
		} catch {
			// Fall back to json() if text() fails.
		}
	}

	if (typeof data.json === 'function') {
		try {
			return JSON.stringify(data.json());
		} catch {
			return null;
		}
	}

	return null;
}

/**
 * Displays a notification for an incoming Web Push event.
 */
export function handlePushEvent(
	event: PushEventLike,
	registration: NotificationRegistrationLike
): void {
	const payload = parsePushPayload(readPushRawText(event.data));

	event.waitUntil(
		registration.showNotification(payload.title, {
			body: payload.body,
			icon: '/favicon.png',
			badge: '/favicon.png',
			...(payload.tag ? { tag: payload.tag } : {}),
			data: { url: payload.url }
		})
	);
}

/**
 * Closes the clicked notification and focuses/navigates an existing app window
 * or opens a new window at the sanitized target route.
 */
export function handleNotificationClick(
	event: NotificationEventLike,
	clients: ClientsLike,
	origin: string
): void {
	event.notification.close();

	const notificationData = event.notification.data as { url?: unknown } | null | undefined;
	const targetUrl = sanitizeNotificationUrl(notificationData?.url, origin);

	event.waitUntil(
		(async () => {
			const windowClients = await clients.matchAll({
				type: 'window',
				includeUncontrolled: true
			});

			const sameOriginClients = windowClients.filter((client) => {
				try {
					return new URL(client.url).origin === origin;
				} catch {
					return false;
				}
			});

			const exactClient = sameOriginClients.find((client) => client.url === targetUrl);
			const chosenClient = exactClient ?? sameOriginClients[0];

			if (chosenClient) {
				if (chosenClient.url !== targetUrl && typeof chosenClient.navigate === 'function') {
					await chosenClient.navigate(targetUrl);
				}
				if (typeof chosenClient.focus === 'function') {
					await chosenClient.focus();
				}
				return;
			}

			await clients.openWindow(targetUrl);
		})()
	);
}
