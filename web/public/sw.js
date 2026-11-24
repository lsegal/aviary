// Aviary's service worker exists for installability and an offline page. It
// never caches the app or its data: every request goes to the network, so a
// deploy is picked up on the next load. Only the "server unreachable" page and
// its icon are cached, as the fallback for navigations that fail.
const CACHE = "aviary-offline-v2";
const OFFLINE_URL = "/offline.html";
const OFFLINE_ASSETS = [OFFLINE_URL, "/icons/icon-192.png"];

self.addEventListener("install", (event) => {
	event.waitUntil(
		caches
			.open(CACHE)
			.then((cache) => cache.addAll(OFFLINE_ASSETS))
			.then(() => self.skipWaiting()),
	);
});

self.addEventListener("activate", (event) => {
	event.waitUntil(
		caches
			.keys()
			.then((keys) =>
				Promise.all(
					keys.filter((key) => key !== CACHE).map((key) => caches.delete(key)),
				),
			)
			.then(() => self.clients.claim()),
	);
});

self.addEventListener("fetch", (event) => {
	const { request } = event;
	if (request.mode !== "navigate") {
		// The offline page's icon is the only subresource served from cache,
		// and only when the network is down.
		if (new URL(request.url).pathname === "/icons/icon-192.png") {
			event.respondWith(fetch(request).catch(() => caches.match(request)));
		}
		return;
	}
	event.respondWith(
		fetch(request).catch(async () => {
			const cache = await caches.open(CACHE);
			return (await cache.match(OFFLINE_URL)) ?? Response.error();
		}),
	);
});
