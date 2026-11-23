import { defineStore } from "pinia";
import { computed, ref } from "vue";

const TOKEN_KEY = "aviary_token";
const RETRY_MIN_MS = 500;
const RETRY_MAX_MS = 10_000;

export const useAuthStore = defineStore("auth", () => {
	// Token is persisted in localStorage (the server also sets an HttpOnly session
	// cookie that is sent automatically with every request, but cannot be read by JS).
	const token = ref<string | null>(localStorage.getItem(TOKEN_KEY));
	// True when the server runs with server.disable_token_auth, so no token is
	// needed and the login prompt is skipped.
	const authDisabled = ref(false);

	const isLoggedIn = computed(() => token.value !== null || authDisabled.value);

	// True while /api/health is unreachable (network error or proxy 5xx) and
	// checkAuthDisabled is retrying; the app shows a reconnecting state then.
	const serverUnavailable = ref(false);
	let pendingCheck: Promise<boolean> | null = null;

	// checkAuthDisabled asks the server whether it needs a token at all. An
	// unreachable server says nothing about auth, so it retries with backoff
	// until the server answers instead of falling back to the login prompt.
	function checkAuthDisabled(): Promise<boolean> {
		pendingCheck ??= pollHealth().finally(() => {
			pendingCheck = null;
		});
		return pendingCheck;
	}

	async function pollHealth(): Promise<boolean> {
		for (
			let delay = RETRY_MIN_MS;
			;
			delay = Math.min(delay * 2, RETRY_MAX_MS)
		) {
			try {
				const res = await fetch("/api/health");
				if (res.status < 500) {
					if (res.ok) {
						const body = (await res.json()) as { auth_disabled?: boolean };
						authDisabled.value = body.auth_disabled === true;
					}
					serverUnavailable.value = false;
					return authDisabled.value;
				}
			} catch {
				// Network error: the server is down; retry below.
			}
			serverUnavailable.value = true;
			await new Promise((resolve) => setTimeout(resolve, delay));
		}
	}

	async function login(inputToken: string): Promise<boolean> {
		const res = await fetch("/api/login", {
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({ token: inputToken }),
		});
		if (res.ok) {
			token.value = inputToken;
			localStorage.setItem(TOKEN_KEY, inputToken);
			return true;
		}
		return false;
	}

	function logout() {
		token.value = null;
		authDisabled.value = false;
		localStorage.removeItem(TOKEN_KEY);
		// biome-ignore lint/suspicious/noDocumentCookie: clearing session cookie on logout
		document.cookie = "aviary_session=; Max-Age=0; path=/";
	}

	function getToken(): string | null {
		return token.value ?? localStorage.getItem(TOKEN_KEY);
	}

	return {
		authDisabled,
		isLoggedIn,
		serverUnavailable,
		token,
		checkAuthDisabled,
		login,
		logout,
		getToken,
	};
});
