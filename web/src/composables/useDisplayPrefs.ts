import { ref } from "vue";
import {
	applyDisplayPrefs,
	DISPLAY_STORAGE_KEY,
	type DisplayPrefs,
	defaultDisplayPrefs,
	normalizeDisplayPrefs,
} from "../lib/displayPrefs";
import {
	clearPreferencesLocally,
	requestPreferences,
	storePreferencesLocally,
} from "../lib/preferencesApi";
import { useAuthStore } from "../stores/auth";

// The user's display preferences (#1093). As with the navigation
// preferences, the server keeps them per user so they follow the user
// across devices, and this browser keeps a copy, applied by index.html
// before the first paint and used whenever the server's can't be read or
// saved. Every change applies at once, with no Save button.

const ENDPOINT = "/api/preferences/display";

function storedPrefs(): DisplayPrefs {
	try {
		return normalizeDisplayPrefs(
			JSON.parse(localStorage.getItem(DISPLAY_STORAGE_KEY) ?? "null"),
		);
	} catch {
		return defaultDisplayPrefs();
	}
}

const prefs = ref<DisplayPrefs>(storedPrefs());
const systemDark = window.matchMedia("(prefers-color-scheme: dark)");
let loaded = false;

function apply() {
	applyDisplayPrefs(prefs.value, systemDark.matches);
}

// The System theme follows the OS as it changes, without a reload.
systemDark.addEventListener("change", apply);
apply();

function show(next: DisplayPrefs) {
	prefs.value = next;
	storePreferencesLocally(DISPLAY_STORAGE_KEY, next);
	apply();
}

export function useDisplayPrefs() {
	if (!loaded && useAuthStore().isLoggedIn) {
		loaded = true;
		void requestPreferences(ENDPOINT, "GET").then((remote) => {
			if (remote) show(normalizeDisplayPrefs(remote));
		});
	}
	return {
		prefs,
		/** Changes some preferences, here and on the server. */
		set(change: Partial<DisplayPrefs>) {
			const next = { ...prefs.value, ...change };
			show(next);
			void requestPreferences(ENDPOINT, "PUT", next);
		},
		/** Restores the default display, here and on the server. */
		reset() {
			show(defaultDisplayPrefs());
			clearPreferencesLocally(DISPLAY_STORAGE_KEY);
			void requestPreferences(ENDPOINT, "DELETE");
		},
	};
}
