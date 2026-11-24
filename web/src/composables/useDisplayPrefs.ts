import { ref } from "vue";
import {
	applyDisplayPrefs,
	DISPLAY_STORAGE_KEY,
	type DisplayPrefs,
	defaultDisplayPrefs,
	normalizeDisplayPrefs,
} from "../lib/displayPrefs";
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

function storeLocally(value: DisplayPrefs) {
	try {
		localStorage.setItem(DISPLAY_STORAGE_KEY, JSON.stringify(value));
	} catch {
		// Private browsing can refuse storage; the server copy still applies.
	}
}

/** The server's reply to a display preferences request, as sent. */
async function request(
	method: "GET" | "PUT" | "DELETE",
	body?: DisplayPrefs,
): Promise<unknown> {
	const token = useAuthStore().getToken();
	try {
		const res = await fetch(ENDPOINT, {
			method,
			headers: {
				...(token ? { Authorization: `Bearer ${token}` } : {}),
				...(body ? { "Content-Type": "application/json" } : {}),
			},
			body: body ? JSON.stringify(body) : undefined,
		});
		if (!res.ok) return undefined;
		return await res.json();
	} catch {
		return undefined;
	}
}

function show(next: DisplayPrefs) {
	prefs.value = next;
	storeLocally(next);
	apply();
}

export function useDisplayPrefs() {
	if (!loaded && useAuthStore().isLoggedIn) {
		loaded = true;
		void request("GET").then((remote) => {
			if (remote) show(normalizeDisplayPrefs(remote));
		});
	}
	return {
		prefs,
		/** Changes some preferences, here and on the server. */
		set(change: Partial<DisplayPrefs>) {
			const next = { ...prefs.value, ...change };
			show(next);
			void request("PUT", next);
		},
		/** Restores the default display, here and on the server. */
		reset() {
			show(defaultDisplayPrefs());
			try {
				localStorage.removeItem(DISPLAY_STORAGE_KEY);
			} catch {
				// Nothing stored.
			}
			void request("DELETE");
		},
	};
}
