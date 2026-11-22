// The Settings page's lazy chunk, shared by its routes and by prefetching.
// The router waits for a route's chunk before it changes the URL, and this
// one is the largest, so it is fetched ahead of the first click (#897).
export const SettingsView = () => import("../views/SettingsView.vue");

/** Starts loading the Settings chunk; the module system dedupes repeats. */
export function prefetchSettingsView() {
	void SettingsView().catch(() => {
		// A failed prefetch leaves the route to load (and report) it.
	});
}

/** Prefetches the Settings chunk once the browser is idle. */
export function prefetchSettingsViewWhenIdle() {
	if ("requestIdleCallback" in window)
		window.requestIdleCallback(prefetchSettingsView, { timeout: 3000 });
	else setTimeout(prefetchSettingsView, 1000);
}
