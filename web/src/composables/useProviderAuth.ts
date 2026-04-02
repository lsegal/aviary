import { computed, onUnmounted, ref } from "vue";

export interface KnownProvider {
	id: string;
	label: string;
	authId: string;
	hasOAuth: boolean;
	hasApiKey: boolean;
}

export const KNOWN_PROVIDERS: KnownProvider[] = [
	{
		id: "anthropic",
		label: "Anthropic",
		authId: "anthropic",
		hasOAuth: true,
		hasApiKey: true,
	},
	{
		id: "openai",
		label: "OpenAI",
		authId: "openai",
		hasOAuth: false,
		hasApiKey: true,
	},
	{
		id: "openai-codex",
		label: "OpenAI Codex",
		authId: "openai",
		hasOAuth: true,
		hasApiKey: false,
	},
	{
		id: "google",
		label: "Google (Gemini)",
		authId: "gemini",
		hasOAuth: true,
		hasApiKey: true,
	},
	{
		id: "github-copilot",
		label: "GitHub Copilot",
		authId: "github-copilot",
		hasOAuth: true,
		hasApiKey: true,
	},
];

type CallTool = (
	name: string,
	args?: Record<string, unknown>,
) => Promise<string>;

export function useProviderAuth(callTool: CallTool) {
	const oauthBusy = ref(false);
	const anthropicUrl = ref("");
	const anthropicCode = ref("");
	const openAIUrl = ref("");
	const openAICallbackUrl = ref("");
	const openAIExpiresAt = ref<number | null>(null);
	const geminiUrl = ref("");
	const geminiCallbackUrl = ref("");
	const geminiExpiresAt = ref<number | null>(null);
	const copilotUserCode = ref("");
	const copilotVerifyUrl = ref("");
	const now = ref(Date.now());
	let countdownTimer: number | null = null;

	function ensureCountdownTimer() {
		if (countdownTimer !== null) {
			return;
		}
		countdownTimer = window.setInterval(() => {
			now.value = Date.now();
			const hasActiveOpenAI =
				openAIExpiresAt.value !== null && openAIExpiresAt.value > now.value;
			const hasActiveGemini =
				geminiExpiresAt.value !== null && geminiExpiresAt.value > now.value;
			if (!hasActiveOpenAI && !hasActiveGemini) {
				clearCountdownTimer();
			}
		}, 1000);
	}

	function clearCountdownTimer() {
		if (countdownTimer !== null) {
			window.clearInterval(countdownTimer);
			countdownTimer = null;
		}
	}

	function parseExpiry(value?: string) {
		const parsed = value ? Date.parse(value) : Number.NaN;
		return Number.isFinite(parsed) ? parsed : null;
	}

	const openAIRemainingSeconds = computed(() =>
		openAIExpiresAt.value
			? Math.max(0, Math.ceil((openAIExpiresAt.value - now.value) / 1000))
			: null,
	);
	const geminiRemainingSeconds = computed(() =>
		geminiExpiresAt.value
			? Math.max(0, Math.ceil((geminiExpiresAt.value - now.value) / 1000))
			: null,
	);
	const openAITimedOut = computed(
		() => openAIExpiresAt.value !== null && openAIRemainingSeconds.value === 0,
	);
	const geminiTimedOut = computed(
		() => geminiExpiresAt.value !== null && geminiRemainingSeconds.value === 0,
	);

	function clearOAuthState() {
		anthropicUrl.value = "";
		anthropicCode.value = "";
		openAIUrl.value = "";
		openAICallbackUrl.value = "";
		openAIExpiresAt.value = null;
		geminiUrl.value = "";
		geminiCallbackUrl.value = "";
		geminiExpiresAt.value = null;
		copilotUserCode.value = "";
		copilotVerifyUrl.value = "";
		clearCountdownTimer();
	}

	async function startAnthropic() {
		oauthBusy.value = true;
		clearOAuthState();
		try {
			const raw = await callTool("auth_login_anthropic");
			const parsed = JSON.parse(raw) as { url?: string; instructions?: string };
			anthropicUrl.value = parsed.url ?? "";
			return parsed.instructions ?? "Anthropic OAuth started.";
		} finally {
			oauthBusy.value = false;
		}
	}

	async function completeAnthropic() {
		if (!anthropicCode.value.trim()) {
			throw new Error("authorization code is required");
		}
		oauthBusy.value = true;
		try {
			return await callTool("auth_login_anthropic_complete", {
				code: anthropicCode.value.trim(),
			});
		} finally {
			oauthBusy.value = false;
		}
	}

	async function startOpenAI() {
		oauthBusy.value = true;
		clearOAuthState();
		try {
			const raw = await callTool("auth_login_openai");
			const parsed = JSON.parse(raw) as {
				url?: string;
				callback_url?: string;
				browser_opened?: boolean;
				browser_open_error?: string;
				expires_at?: string;
			};
			openAIUrl.value = parsed.url ?? "";
			openAICallbackUrl.value = parsed.callback_url ?? "";
			openAIExpiresAt.value = parseExpiry(parsed.expires_at);
			ensureCountdownTimer();
			return parsed;
		} finally {
			oauthBusy.value = false;
		}
	}

	async function completeOpenAI() {
		oauthBusy.value = true;
		try {
			return await callTool("auth_login_openai_complete");
		} finally {
			oauthBusy.value = false;
		}
	}

	async function startGemini() {
		oauthBusy.value = true;
		clearOAuthState();
		try {
			const raw = await callTool("auth_login_gemini");
			const parsed = JSON.parse(raw) as {
				url?: string;
				callback_url?: string;
				browser_opened?: boolean;
				browser_open_error?: string;
				expires_at?: string;
			};
			geminiUrl.value = parsed.url ?? "";
			geminiCallbackUrl.value = parsed.callback_url ?? "";
			geminiExpiresAt.value = parseExpiry(parsed.expires_at);
			ensureCountdownTimer();
			return parsed;
		} finally {
			oauthBusy.value = false;
		}
	}

	async function completeGemini() {
		oauthBusy.value = true;
		try {
			return await callTool("auth_login_gemini_complete");
		} finally {
			oauthBusy.value = false;
		}
	}

	async function startCopilot() {
		oauthBusy.value = true;
		clearOAuthState();
		try {
			const raw = await callTool("auth_login_github_copilot");
			const parsed = JSON.parse(raw) as {
				user_code?: string;
				verification_uri?: string;
			};
			copilotUserCode.value = parsed.user_code ?? "";
			copilotVerifyUrl.value = parsed.verification_uri ?? "";
			return parsed;
		} finally {
			oauthBusy.value = false;
		}
	}

	async function completeCopilot() {
		oauthBusy.value = true;
		try {
			return await callTool("auth_login_github_copilot_complete");
		} finally {
			oauthBusy.value = false;
		}
	}

	onUnmounted(() => {
		clearCountdownTimer();
	});

	return {
		oauthBusy,
		anthropicUrl,
		anthropicCode,
		openAIUrl,
		openAICallbackUrl,
		openAIExpiresAt,
		openAIRemainingSeconds,
		openAITimedOut,
		geminiUrl,
		geminiCallbackUrl,
		geminiExpiresAt,
		geminiRemainingSeconds,
		geminiTimedOut,
		copilotUserCode,
		copilotVerifyUrl,
		clearOAuthState,
		startAnthropic,
		completeAnthropic,
		startOpenAI,
		completeOpenAI,
		startGemini,
		completeGemini,
		startCopilot,
		completeCopilot,
	};
}
