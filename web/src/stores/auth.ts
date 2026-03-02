import { defineStore } from "pinia";
import { computed, ref } from "vue";

const TOKEN_KEY = "aviary_token";

export const useAuthStore = defineStore("auth", () => {
	// Token is persisted in localStorage (the server also sets an HttpOnly session
	// cookie that is sent automatically with every request, but cannot be read by JS).
	const token = ref<string | null>(localStorage.getItem(TOKEN_KEY));

	const isLoggedIn = computed(() => token.value !== null);

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
		localStorage.removeItem(TOKEN_KEY);
		document.cookie = "aviary_session=; Max-Age=0; path=/";
	}

	function getToken(): string | null {
		return token.value ?? localStorage.getItem(TOKEN_KEY);
	}

	return { isLoggedIn, token, login, logout, getToken };
});
