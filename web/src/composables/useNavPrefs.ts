import { ref } from "vue";
import {
	emptyNavPrefs,
	hasStaleNavLists,
	type NavList,
	type NavPrefs,
	normalizeNavPrefs,
	withOrder,
} from "../lib/navPrefs";
import {
	clearPreferencesLocally,
	requestPreferences,
	storePreferencesLocally,
} from "../lib/preferencesApi";
import { useAuthStore } from "../stores/auth";

// The user's navigation order and visibility (#1003). The server keeps them
// per user so they follow the user across devices; this browser keeps a
// copy, read before the first render and used whenever the server's can't
// be read or saved.

const STORAGE_KEY = "aviary.nav.prefs";
const ENDPOINT = "/api/preferences/navigation";

function storedPrefs(): NavPrefs {
	try {
		const raw = localStorage.getItem(STORAGE_KEY);
		if (!raw) return emptyNavPrefs();
		const parsed = JSON.parse(raw);
		const prefs = normalizeNavPrefs(parsed);
		if (hasStaleNavLists(parsed)) storePreferencesLocally(STORAGE_KEY, prefs);
		return prefs;
	} catch {
		return emptyNavPrefs();
	}
}

const prefs = ref<NavPrefs>(storedPrefs());
/** Shows hidden items in the sidebar and top row for this visit. */
const showHidden = ref(false);
let loaded = false;

function save(next: NavPrefs) {
	prefs.value = next;
	storePreferencesLocally(STORAGE_KEY, next);
	void requestPreferences(ENDPOINT, "PUT", next);
}

export function useNavPrefs() {
	if (!loaded && useAuthStore().isLoggedIn) {
		loaded = true;
		void requestPreferences(ENDPOINT, "GET").then((remote) => {
			if (!remote) return;
			const next = normalizeNavPrefs(remote);
			// Drop a saved top-row order (#1015) from the server's copy too.
			if (hasStaleNavLists(remote)) save(next);
			else {
				prefs.value = next;
				storePreferencesLocally(STORAGE_KEY, next);
			}
		});
	}
	return {
		prefs,
		showHidden,
		/** Saves the list's order, given the keys it shows now in order. */
		setOrder(list: NavList, shown: string[]) {
			const order = withOrder(prefs.value.order[list], shown);
			save({ ...prefs.value, order: { ...prefs.value.order, [list]: order } });
		},
		isHidden(list: NavList, key: string): boolean {
			return prefs.value.hidden[list]?.includes(key) ?? false;
		},
		setHidden(list: NavList, key: string, hide: boolean) {
			const rest = (prefs.value.hidden[list] ?? []).filter((k) => k !== key);
			const hidden = hide ? [...rest, key] : rest;
			save({
				...prefs.value,
				hidden: { ...prefs.value.hidden, [list]: hidden },
			});
		},
		/** Restores the built-in order and visibility, here and on the server. */
		reset() {
			prefs.value = emptyNavPrefs();
			clearPreferencesLocally(STORAGE_KEY);
			void requestPreferences(ENDPOINT, "DELETE");
		},
	};
}
