/// <reference types="@sveltejs/kit" />
/// <reference no-default-lib="true"/>
/// <reference lib="esnext" />
/// <reference lib="webworker" />

import { build, files, version } from '$service-worker';

const sw = self as unknown as ServiceWorkerGlobalScope;
const CACHE_NAME = `budget-app-cache-${version}`;
const ASSETS = [...build, ...files];

sw.addEventListener('install', (event) => {
	event.waitUntil(
		caches
			.open(CACHE_NAME)
			.then((cache) => cache.addAll(ASSETS))
			.then(() => sw.skipWaiting())
	);
});

sw.addEventListener('activate', (event) => {
	event.waitUntil(
		caches
			.keys()
			.then((keys) => Promise.all(keys.filter((key) => key !== CACHE_NAME).map((key) => caches.delete(key))))
			.then(() => sw.clients.claim())
	);
});

sw.addEventListener('fetch', (event) => {
	if (event.request.method !== 'GET') return;

	const url = new URL(event.request.url);
	if (url.pathname.startsWith('/api/')) return;

	event.respondWith(
		(async () => {
			const cache = await caches.open(CACHE_NAME);
			if (ASSETS.includes(url.pathname)) {
				const cached = await cache.match(url.pathname);
				if (cached) return cached;
			}

			try {
				const response = await fetch(event.request);
				if (response.status === 200 && url.origin === sw.location.origin) {
					void cache.put(event.request, response.clone());
				}
				return response;
			} catch {
				const cached = await cache.match(event.request);
				if (cached) return cached;
				const fallback = await cache.match('/');
				if (fallback) return fallback;
				return new Response('Offline', { status: 503, statusText: 'Offline' });
			}
		})()
	);
});

interface SyncEvent extends ExtendableEvent {
	tag: string;
}

sw.addEventListener('sync', ((event: SyncEvent) => {
	if (event.tag === 'sync-transactions') {
		event.waitUntil(
			sw.clients.matchAll({ includeUncontrolled: true, type: 'window' }).then((clients) => {
				for (const client of clients) {
					client.postMessage({ type: 'SYNC_TRANSACTIONS_REQUESTED' });
				}
			})
		);
	}
}) as EventListener);
