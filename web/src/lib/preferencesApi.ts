import { useAuthStore } from "../stores/auth";

// Shared plumbing for per-user preferences that the server keeps and this
// browser mirrors in localStorage (navigation #1003, display #1093).

/** The server's reply to a preferences request, as sent. */
export async function requestPreferences<T>(
	endpoint: string,
	method: "GET" | "PUT" | "DELETE",
	body?: T,
): Promise<unknown> {
	const token = useAuthStore().getToken();
	try {
		const res = await fetch(endpoint, {
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

/** Keeps this browser's copy of some preferences. */
export function storePreferencesLocally<T>(key: string, value: T) {
	try {
		localStorage.setItem(key, JSON.stringify(value));
	} catch {
		// Private browsing can refuse storage; the server copy still applies.
	}
}

/** Forgets this browser's copy of some preferences. */
export function clearPreferencesLocally(key: string) {
	try {
		localStorage.removeItem(key);
	} catch {
		// Nothing stored.
	}
}
