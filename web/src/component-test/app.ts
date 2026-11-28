// Mounts the whole web app in a component test (#1636), at a route, against
// the fake server in ./server. Each test file runs in its own page, so
// module state starts fresh per file; within a file, every test mounts a new
// app with a new Pinia store and the router at the test's route.
//
// Usage:
//   mockMCP({ agent_list: [...] });
//   await renderApp("/tasks");
//   await expect.element(page.getByTestId("tasks-overview")).toBeVisible();

import { createPinia } from "pinia";
import { createApp, nextTick, type App as VueApp } from "vue";
import App from "../App.vue";
import router from "../router";
import { whenIdle } from "./server";

let app: VueApp | null = null;
let container: HTMLElement | null = null;

export interface RenderOptions {
	/** The token stored as signed in; null signs the page out. */
	token?: string | null;
	/** Leaves the setup wizard to run instead of counting it skipped. */
	onboarding?: boolean;
}

/** Signs the page in, as the e2e setAuthToken helper does. Unless
 * `onboarding` is set, the setup wizard counts as skipped. */
export function setAuthToken(
	token: string | null = "test-token",
	{ onboarding = false } = {},
): void {
	if (token === null) {
		// Signed out, as a page that never called setAuthToken.
		localStorage.removeItem("aviary_token");
		sessionStorage.removeItem("aviary_onboarding_skipped");
		return;
	}
	localStorage.setItem("aviary_token", token);
	if (onboarding) sessionStorage.removeItem("aviary_onboarding_skipped");
	else sessionStorage.setItem("aviary_onboarding_skipped", "1");
}

/** Mounts the app at `url` (signed in unless options say otherwise) and
 * waits for the route to resolve. Calling it again replaces the app. */
export async function renderApp(
	url = "/",
	options: RenderOptions = {},
): Promise<void> {
	await settleAndUnmount();
	if (options.token !== undefined || options.onboarding !== undefined) {
		setAuthToken(options.token === undefined ? "test-token" : options.token, {
			onboarding: options.onboarding,
		});
	} else if (!localStorage.getItem("aviary_token")) {
		setAuthToken();
	}
	// A fresh page has no history state; the last mount's (vue-router's
	// back and forward entries among it) would leak into this one.
	history.replaceState(null, "", url);
	container = document.createElement("div");
	container.id = "app";
	document.body.appendChild(container);
	app = createApp(App);
	app.use(createPinia());
	app.use(router);
	await router.replace(url).catch(() => {});
	await router.isReady();
	app.mount(container);
	await nextTick();
}

/** Navigates the mounted app to `url`, as page.goto would. */
export async function goto(url: string): Promise<void> {
	if (!app) return renderApp(url);
	await router.push(url).catch(() => {});
	await nextTick();
}

/** Remounts the app at its current URL, as page.reload does. Server state
 * kept by routes and browser storage carry over; module state does too. */
export async function reload(): Promise<void> {
	await renderApp(currentURL(), {
		token: localStorage.getItem("aviary_token"),
	});
}

/** The current path, query and hash. */
export function currentURL(): string {
	return location.pathname + location.search + location.hash;
}

/** Goes back one history entry and waits for the router. */
export async function goBack(): Promise<void> {
	const done = new Promise<void>((resolve) =>
		router.afterEach(() => resolve()),
	);
	router.back();
	await Promise.race([done, new Promise((r) => setTimeout(r, 1000))]);
	await nextTick();
}

/** Goes forward one history entry and waits for the router. */
export async function goForward(): Promise<void> {
	const done = new Promise<void>((resolve) =>
		router.afterEach(() => resolve()),
	);
	router.forward();
	await Promise.race([done, new Promise((r) => setTimeout(r, 1000))]);
	await nextTick();
}

/**
 * Lets the mounted app finish what it started, then unmounts it. A page's
 * answering requests, lazy pages and redirects can still navigate the router
 * every mount shares, as a page.goto or a closed page would have discarded;
 * this runs them on the old app, until no request is in flight and the
 * route has held still for a moment.
 */
export async function settleAndUnmount(): Promise<void> {
	if (app) {
		const deadline = performance.now() + 3_000;
		let route = router.currentRoute.value.fullPath;
		while (performance.now() < deadline) {
			await whenIdle();
			await new Promise((resolve) => setTimeout(resolve, 50));
			const now = router.currentRoute.value.fullPath;
			if (now === route) break;
			route = now;
		}
	}
	unmountApp();
}

/** Unmounts the app, if one is mounted. */
export function unmountApp(): void {
	app?.unmount();
	container?.remove();
	app = null;
	container = null;
}

export { router };
