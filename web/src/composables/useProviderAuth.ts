import { computed, onMounted, onUnmounted, type Ref, ref } from "vue";
import { type CLIProvider, normalizeCLIList } from "../lib/cliProviders";
import { parseToolJSON } from "../lib/projects";
import { useMCP } from "./useMCP";

export interface KnownProvider {
	id: string;
	label: string;
	authId: string;
	hasOAuth: boolean;
	hasApiKey: boolean;
	emoji?: string;
	description?: string;
	keyPlaceholder?: string;
	keyHelp?: string;
	apiAuthKey?: string;
	defaultModel?: string;
	oauthModel?: string;
	defaultProviders?: string[];
	oauthProviders?: string[];
	authKeys?: string[];
	requiresBaseURI?: boolean;
	baseURIPlaceholder?: string;
}

/** What sets apart a provider offering both OAuth sign-in and an API key. */
interface SignInProviderSpec {
	id: string;
	label: string;
	authId: string;
	emoji: string;
	description: string;
	keyPlaceholder: string;
	keyHelp: string;
	/** The default model, with either kind of credential. */
	model: string;
	/** The model providers, with either kind of credential. */
	providers: string[];
}

/**
 * A provider that signs in with OAuth or an API key, using the same model
 * and model providers either way and the auth keys `<authId>:oauth` and
 * `<authId>:default`.
 */
function signInProvider(spec: SignInProviderSpec): KnownProvider {
	const { model, providers, ...rest } = spec;
	return {
		...rest,
		hasOAuth: true,
		hasApiKey: true,
		apiAuthKey: `${spec.authId}:default`,
		defaultModel: model,
		oauthModel: model,
		defaultProviders: [...providers],
		oauthProviders: [...providers],
		authKeys: [`${spec.authId}:oauth`, `${spec.authId}:default`],
	};
}

export const KNOWN_PROVIDERS: KnownProvider[] = [
	signInProvider({
		id: "anthropic",
		label: "Anthropic",
		authId: "anthropic",
		emoji: "🤖",
		description: "Claude - great for coding and reasoning",
		keyPlaceholder: "sk-ant-...",
		keyHelp: "Find your key at console.anthropic.com -> API Keys.",
		model: "anthropic/claude-sonnet-5-5",
		providers: ["anthropic"],
	}),
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
		emoji: "🧠",
		description: "ChatGPT sign-in for OpenAI Codex models",
		keyPlaceholder: "sk-...",
		keyHelp: "Find your key at platform.openai.com -> API keys.",
		apiAuthKey: "openai:default",
		defaultModel: "openai/gpt-6.2-sol",
		oauthModel: "openai-codex/gpt-6.2-astra",
		defaultProviders: ["openai"],
		oauthProviders: ["openai-codex"],
		authKeys: ["openai:oauth", "openai:default"],
	},
	signInProvider({
		id: "google",
		label: "Google (Gemini)",
		authId: "gemini",
		emoji: "✨",
		description: "Google Gemini - fast and multimodal",
		keyPlaceholder: "AIza...",
		keyHelp: "Find your key at aistudio.google.com -> Get API key.",
		model: "google-gemini/gemini-3.5-flash",
		providers: ["google-gemini", "google"],
	}),
	{
		id: "meta",
		label: "Meta (Muse)",
		authId: "meta",
		hasOAuth: false,
		hasApiKey: true,
		emoji: "🎼",
		description: "Meta Muse models through the Model API",
		keyPlaceholder: "LLM_...",
		keyHelp: "Find your key at dev.meta.ai -> API keys.",
		apiAuthKey: "meta:default",
		defaultModel: "meta/muse-spark-1.3",
		defaultProviders: ["meta"],
		authKeys: ["meta:default"],
	},
	{
		id: "vllm",
		label: "vLLM",
		authId: "vllm",
		hasOAuth: false,
		hasApiKey: true,
		emoji: "🛠️",
		description: "Self-hosted OpenAI-compatible inference via vLLM",
		keyPlaceholder: "Bearer token (optional)",
		keyHelp: "Leave empty if your vLLM server does not require authentication.",
		apiAuthKey: "vllm:default",
		defaultProviders: ["vllm"],
		authKeys: ["vllm:default"],
		requiresBaseURI: true,
		baseURIPlaceholder: "http://127.0.0.1:8000",
	},
	{
		id: "ollama",
		label: "Ollama",
		authId: "ollama",
		hasOAuth: false,
		hasApiKey: true,
		emoji: "🦙",
		description: "Local or remote Ollama via its OpenAI-compatible API",
		keyPlaceholder: "Bearer token (optional)",
		keyHelp:
			"Leave empty if your Ollama server does not require authentication.",
		apiAuthKey: "ollama:default",
		defaultProviders: ["ollama"],
		authKeys: ["ollama:default"],
		requiresBaseURI: true,
		baseURIPlaceholder: "http://127.0.0.1:11434",
	},
	signInProvider({
		id: "github-copilot",
		label: "GitHub Copilot",
		authId: "github-copilot",
		emoji: "🐦",
		description: "GitHub Copilot - code-specialized models",
		keyPlaceholder: "ghp_... or personal access token",
		keyHelp:
			"Use a GitHub Personal Access Token (repo scope) or sign in via OAuth.",
		model: "github-copilot/gpt-5.4",
		providers: ["github-copilot"],
	}),
];

/** The reply that starts a sign-in through a local callback server. */
interface CallbackOAuthStart {
	url?: string;
	callback_url?: string;
	browser_opened?: boolean;
	browser_open_error?: string;
	expires_at?: string;
}

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

	/** Starts a sign-in that finishes on a local callback, with a countdown. */
	async function startCallbackOAuth(
		tool: string,
		target: {
			url: Ref<string>;
			callbackUrl: Ref<string>;
			expiresAt: Ref<number | null>;
		},
	) {
		oauthBusy.value = true;
		clearOAuthState();
		try {
			const raw = await callTool(tool);
			const parsed = JSON.parse(raw) as CallbackOAuthStart;
			target.url.value = parsed.url ?? "";
			target.callbackUrl.value = parsed.callback_url ?? "";
			target.expiresAt.value = parseExpiry(parsed.expires_at);
			ensureCountdownTimer();
			return parsed;
		} finally {
			oauthBusy.value = false;
		}
	}

	function startOpenAI() {
		return startCallbackOAuth("auth_login_openai", {
			url: openAIUrl,
			callbackUrl: openAICallbackUrl,
			expiresAt: openAIExpiresAt,
		});
	}

	async function completeOpenAI() {
		oauthBusy.value = true;
		try {
			return await callTool("auth_login_openai_complete");
		} finally {
			oauthBusy.value = false;
		}
	}

	function startGemini() {
		return startCallbackOAuth("auth_login_gemini", {
			url: geminiUrl,
			callbackUrl: geminiCallbackUrl,
			expiresAt: geminiExpiresAt,
		});
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

const CLI_PROVIDERS_CACHE_KEY = "aviary.providers.clis";

// Providers reached through a CLI report their install, sign-in, quota
// and model state from core's provider_cli_list. The probe is slow (it runs
// every CLI's doctor), so its last result is shared across components and kept
// in localStorage: the Providers list renders from the cache at once and the
// probe refreshes it in the background.
const cliProviders = ref<CLIProvider[] | null>(readCLIProviderCache());
const cliProvidersRefreshing = ref(false);
const cliProvidersError = ref("");
let cliProvidersInflight: Promise<void> | null = null;

function readCLIProviderCache(): CLIProvider[] | null {
	try {
		const raw = localStorage.getItem(CLI_PROVIDERS_CACHE_KEY);
		return raw ? normalizeCLIList(JSON.parse(raw)) : null;
	} catch {
		return null;
	}
}

/**
 * The state of the providers reached through a CLI: the cached result
 * at once, refreshed in the background when the caller mounts unless it asks to
 * refresh on its own schedule.
 */
export function useProviderStatus({ refreshOnMount = true } = {}) {
	const { callTool } = useMCP();

	function refresh(): Promise<void> {
		if (cliProvidersInflight) return cliProvidersInflight;
		cliProvidersRefreshing.value = true;
		cliProvidersInflight = (async () => {
			try {
				const clis = normalizeCLIList(
					parseToolJSON(await callTool("provider_cli_list", {})),
				);
				cliProviders.value = clis;
				cliProvidersError.value = "";
				try {
					localStorage.setItem(CLI_PROVIDERS_CACHE_KEY, JSON.stringify(clis));
				} catch {
					// Storage full or disabled; the in-memory cache still works.
				}
			} catch (e) {
				cliProvidersError.value = e instanceof Error ? e.message : String(e);
			} finally {
				cliProvidersRefreshing.value = false;
				cliProvidersInflight = null;
			}
		})();
		return cliProvidersInflight;
	}

	if (refreshOnMount) onMounted(() => void refresh());

	return {
		clis: cliProviders,
		refreshing: cliProvidersRefreshing,
		error: cliProvidersError,
		refresh,
	};
}
