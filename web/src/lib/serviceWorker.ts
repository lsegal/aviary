/**
 * Registers the service worker that makes Aviary installable and shows the
 * "server unreachable" page offline. Production only, so the dev server never
 * runs a worker, and only on secure origins, where browsers allow one.
 */
export function registerServiceWorker(): void {
	if (!import.meta.env.PROD || !window.isSecureContext) return;
	if (!("serviceWorker" in navigator)) return;
	window.addEventListener("load", () => {
		navigator.serviceWorker
			.register("/sw.js", { scope: "/", updateViaCache: "none" })
			.catch((err) => console.warn("service worker registration failed", err));
	});
}
