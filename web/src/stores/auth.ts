import { defineStore } from "pinia";
import { computed, ref } from "vue";

const TOKEN_KEY = "aviary_token";

export const useAuthStore = defineStore("auth", () => {
	// Token is persisted in localStorage (the server also sets an HttpOnly session
	// cookie that is sent automatically with every request, but cannot be read by JS).
	const token = ref<string | null>(localStorage.getItem(TOKEN_KEY));
	// True when the server runs with server.disable_token_auth, so no token is
	// needed and the login prompt is skipped.
	const authDisabled = ref(false);

	const isLoggedIn = computed(() => token.value !== null || authDisabled.value);

	// checkAuthDisabled asks the server whether it needs a token at all.
	async function checkAuthDisabled(): Promise<boolean> {
		try {
			const res = await fetch("/api/health");
			if (res.ok) {
				const body = (await res.json()) as { auth_disabled?: boolean };
				authDisabled.value = body.auth_disabled === true;
			}
		} catch {
			// Unreachable server: keep requiring a token.
		}
		return authDisabled.value;
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
		token,
		checkAuthDisabled,
		login,
		logout,
		getToken,
	};
});
