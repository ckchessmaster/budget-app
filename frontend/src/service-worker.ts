/// <reference lib="webworker" />
import { build, files, version } from '$service-worker';

const sw = self as unknown as ServiceWorkerGlobalScope;
const CACHE_NAME = `budget-cache-${version}`;
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
		caches.keys().then(async (keys) => {
			for (const key of keys) {
				if (key !== CACHE_NAME) {
					await caches.delete(key);
				}
			}
			await sw.clients.claim();
		})
	);
});

sw.addEventListener('fetch', (event) => {
	if (event.request.method !== 'GET') return;

	const url = new URL(event.request.url);
	if (url.pathname.startsWith('/api/')) return;

	event.respondWith(
		caches.match(event.request).then((cached) => {
			if (cached) return cached;
			return fetch(event.request).catch(() => caches.match('/index.html') as Promise<Response>);
		})
	);
});

sw.addEventListener('sync', (event: any) => {
	if (event.tag === 'sync-transactions') {
		event.waitUntil(
			sw.clients.matchAll({ type: 'window' }).then((clients) => {
				for (const client of clients) {
					client.postMessage({ type: 'SYNC_TRANSACTIONS' });
				}
			})
		);
	}
});
