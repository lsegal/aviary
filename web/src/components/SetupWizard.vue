<template>
	<div class="mx-auto max-w-2xl px-4 py-12">
		<!-- Progress dots -->
		<div class="mb-10 flex items-center justify-center gap-2">
			<div v-for="(s, i) in steps" :key="s" class="flex items-center gap-2">
				<div :class="[
					'flex h-7 w-7 items-center justify-center rounded-full text-xs font-semibold transition-colors',
					currentStepIndex > i
						? 'bg-blue-600 text-white'
						: currentStepIndex === i
							? 'bg-blue-600 text-white ring-4 ring-blue-100 dark:ring-blue-900/40'
							: 'bg-gray-200 text-gray-500 dark:bg-gray-700 dark:text-gray-400',
				]">
					<svg v-if="currentStepIndex > i" class="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor"
						stroke-width="3">
						<path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" />
					</svg>
					<span v-else>{{ i + 1 }}</span>
				</div>
				<div v-if="i < steps.length - 1" class="h-px w-8 bg-gray-200 dark:bg-gray-700" />
			</div>
		</div>

		<!-- Step 1: Choose provider -->
		<div v-if="step === 'provider'" class="text-center">
			<h1 class="mb-2 text-2xl font-bold text-gray-900 dark:text-white">Welcome to Aviary</h1>
			<p class="mb-8 text-sm text-gray-500 dark:text-gray-400">Let's get you set up in under a minute. Which AI provider
				would you like to use?</p>

			<div class="grid gap-3 sm:grid-cols-3">
				<button v-for="p in providers" :key="p.id" type="button" :class="[
					'flex flex-col items-center gap-3 rounded-xl border-2 p-5 transition-all hover:shadow-md',
					selectedProvider === p.id
						? 'border-blue-500 bg-blue-50 dark:bg-blue-950/30'
						: 'border-gray-200 bg-white hover:border-gray-300 dark:border-gray-700 dark:bg-gray-900 dark:hover:border-gray-600',
				]" @click="selectProvider(p.id)">
					<span class="text-3xl">{{ p.emoji }}</span>
					<div>
						<p class="font-semibold text-gray-900 dark:text-white">{{ p.name }}</p>
						<p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">{{ p.description }}</p>
					</div>
					<span v-if="detectedAuth(p)"
						class="rounded-full bg-green-100 px-2 py-0.5 text-xs font-medium text-green-700 dark:bg-green-900/40 dark:text-green-400">
						{{ detectedAuth(p) === "oauth" ? "signed in" : "key set" }}
					</span>
				</button>
			</div>

			<div class="mt-8 flex items-center justify-between">
				<button type="button" class="text-xs text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
					@click="$emit('skip')">
					Skip for now
				</button>
				<button type="button" :disabled="!selectedProvider"
					class="rounded-lg bg-blue-600 px-5 py-2.5 text-sm font-semibold text-white hover:bg-blue-500 disabled:opacity-40"
					@click="step = 'credentials'">
					Continue →
				</button>
			</div>
		</div>

		<!-- Step 2: Connect credentials -->
		<div v-else-if="step === 'credentials'">
			<button type="button"
				class="mb-6 flex items-center gap-1.5 text-xs text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
				@click="step = 'provider'">
				← Back
			</button>

			<div class="mb-6 text-center">
				<span class="mb-3 inline-block text-4xl">{{ currentProvider?.emoji }}</span>
				<h2 class="text-xl font-bold text-gray-900 dark:text-white">Connect {{ currentProvider?.name }}</h2>
			</div>

			<!-- Method tabs (only shown if provider supports both) -->
			<div v-if="currentProvider?.oauth"
				class="mb-5 flex rounded-lg border border-gray-200 bg-gray-50 p-1 dark:border-gray-700 dark:bg-gray-800/50">
				<button type="button"
					:class="['flex-1 rounded-md py-1.5 text-xs font-semibold transition-colors', credMethod === 'oauth' ? 'bg-white text-gray-900 shadow-sm dark:bg-gray-900 dark:text-white' : 'text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200']"
					@click="credMethod = 'oauth'">Sign in with {{ currentProvider.name }}</button>
				<button type="button"
					:class="['flex-1 rounded-md py-1.5 text-xs font-semibold transition-colors', credMethod === 'apikey' ? 'bg-white text-gray-900 shadow-sm dark:bg-gray-900 dark:text-white' : 'text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200']"
					@click="credMethod = 'apikey'">API key</button>
			</div>

			<!-- OAuth panel -->
			<div v-if="credMethod === 'oauth'"
				class="rounded-xl border border-gray-200 bg-white p-6 dark:border-gray-700 dark:bg-gray-900">
				<!-- Anthropic: URL + code exchange -->
				<template v-if="currentProvider?.id === 'anthropic'">
					<div v-if="!oauthUrl">
						<p class="mb-4 text-sm text-gray-600 dark:text-gray-400">
							Sign in with your Claude Pro or Claude Max account — no API key needed.
							We'll open Anthropic's authorization page for you.
						</p>
						<button type="button" :disabled="credSaving"
							class="w-full rounded-lg bg-orange-500 px-4 py-2.5 text-sm font-semibold text-white hover:bg-orange-400 disabled:opacity-40"
							@click="startAnthropicOAuth">
							{{ credSaving ? 'Opening…' : 'Sign in with Anthropic →' }}
						</button>
					</div>
					<div v-else class="space-y-4">
						<p class="text-sm text-gray-700 dark:text-gray-300">
							A browser tab should have opened. Complete sign-in there, then copy the authorization code shown and paste
							it below.
						</p>
						<div
							class="flex items-center gap-2 rounded-lg border border-gray-200 bg-gray-50 px-3 py-2 dark:border-gray-700 dark:bg-gray-800">
							<a :href="oauthUrl" target="_blank" rel="noreferrer"
								class="flex-1 truncate text-xs text-blue-600 hover:underline dark:text-blue-400">{{ oauthUrl }}</a>
							<a :href="oauthUrl" target="_blank" rel="noreferrer"
								class="shrink-0 text-xs text-gray-400 hover:text-gray-600 dark:hover:text-gray-300">Open ↗</a>
						</div>
						<div>
							<label class="mb-1.5 block text-xs font-medium text-gray-600 dark:text-gray-400">Authorization
								code</label>
							<input v-model="oauthCode" type="text" placeholder="Paste code here…"
								class="w-full rounded-lg border border-gray-300 bg-white px-3 py-2.5 text-sm text-gray-900 placeholder-gray-400 focus:border-blue-500 focus:outline-none dark:border-gray-700 dark:bg-gray-800 dark:text-white dark:placeholder-gray-500"
								@keyup.enter="completeAnthropicOAuth" />
						</div>
						<button type="button" :disabled="!oauthCode.trim() || credSaving"
							class="w-full rounded-lg bg-orange-500 px-4 py-2.5 text-sm font-semibold text-white hover:bg-orange-400 disabled:opacity-40"
							@click="completeAnthropicOAuth">
							{{ credSaving ? 'Verifying…' : 'Complete sign-in →' }}
						</button>
					</div>
				</template>

				<!-- OpenAI: blocking browser redirect -->
				<template v-else-if="currentProvider?.id === 'openai'">
					<div v-if="!credSaving">
						<p class="mb-4 text-sm text-gray-600 dark:text-gray-400">
							Sign in with your ChatGPT Plus or Pro account. We'll open OpenAI's consent page;
							after approving, you'll be redirected back automatically.
						</p>
						<p class="mb-4 text-xs text-gray-500 dark:text-gray-400">
							This uses the <code class="font-mono">openai-codex</code> provider and requires a ChatGPT Plus/Pro
							subscription.
						</p>
						<button type="button"
							class="w-full rounded-lg bg-green-600 px-4 py-2.5 text-sm font-semibold text-white hover:bg-green-500"
							@click="startOpenAIOAuth">
							Sign in with OpenAI →
						</button>
					</div>
					<div v-else class="flex flex-col items-center gap-4 py-4 text-center">
						<svg class="h-8 w-8 animate-spin text-green-600" fill="none" viewBox="0 0 24 24">
							<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" />
							<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
						</svg>
						<p class="text-sm text-gray-600 dark:text-gray-400">Waiting for browser sign-in… <br><span
								class="text-xs">Complete the flow in the browser tab that opened.</span></p>
					</div>
				</template>
				<!-- Gemini: blocking browser redirect -->
				<template v-else-if="currentProvider?.id === 'gemini'">
					<div v-if="!credSaving">
						<p class="mb-4 text-sm text-gray-600 dark:text-gray-400">
							Sign in with your Google account. We'll open Google's consent page;
							after approving, you'll be redirected back automatically.
						</p>
						<button type="button"
							class="w-full rounded-lg bg-blue-500 px-4 py-2.5 text-sm font-semibold text-white hover:bg-blue-400"
							@click="startGeminiOAuth">
							Sign in with Google →
						</button>
					</div>
					<div v-else class="flex flex-col items-center gap-4 py-4 text-center">
						<svg class="h-8 w-8 animate-spin text-blue-500" fill="none" viewBox="0 0 24 24">
							<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" />
							<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
						</svg>
						<p class="text-sm text-gray-600 dark:text-gray-400">Waiting for browser sign-in… <br><span
								class="text-xs">Complete the flow in the browser tab that opened.</span></p>
					</div>
				</template>

			<!-- GitHub Copilot: device flow -->
			<template v-else-if="currentProvider?.id === 'github-copilot'">
				<div v-if="!copilotUserCode && !credSaving">
					<p class="mb-4 text-sm text-gray-600 dark:text-gray-400">
						Sign in with your GitHub account that has Copilot access.
						No browser redirect needed — we'll show you a short code to enter on GitHub.
					</p>
					<button type="button"
						class="w-full rounded-lg bg-gray-900 px-4 py-2.5 text-sm font-semibold text-white hover:bg-gray-700 dark:bg-gray-700 dark:hover:bg-gray-600"
						@click="startCopilotOAuth">
						Sign in with GitHub →
					</button>
				</div>
				<div v-else-if="copilotUserCode" class="space-y-4">
					<p class="text-sm text-gray-700 dark:text-gray-300">
						Visit <a :href="copilotVerifyUrl" target="_blank" rel="noreferrer"
							class="font-medium text-blue-600 hover:underline dark:text-blue-400">{{ copilotVerifyUrl }}</a>
						and enter this code:
					</p>
					<div class="flex items-center justify-center rounded-lg border border-gray-200 bg-gray-50 py-4 dark:border-gray-700 dark:bg-gray-800">
						<span class="font-mono text-2xl font-bold tracking-widest text-gray-900 dark:text-white">{{ copilotUserCode }}</span>
					</div>
					<button type="button" :disabled="credSaving"
						class="w-full rounded-lg bg-gray-900 px-4 py-2.5 text-sm font-semibold text-white hover:bg-gray-700 disabled:opacity-40 dark:bg-gray-700 dark:hover:bg-gray-600"
						@click="completeCopilotOAuth">
						<span v-if="credSaving" class="flex items-center justify-center gap-2">
							<svg class="h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
								<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" />
								<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
							</svg>
							Waiting for authorization…
						</span>
						<span v-else>I've authorized — Continue →</span>
					</button>
				</div>
				<div v-else class="flex flex-col items-center gap-3 py-4 text-center">
					<svg class="h-8 w-8 animate-spin text-gray-600" fill="none" viewBox="0 0 24 24">
						<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" />
						<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
					</svg>
					<p class="text-sm text-gray-500 dark:text-gray-400">Requesting device code…</p>
				</div>
			</template>
			</div>

			<!-- API key panel -->
			<div v-else class="rounded-xl border border-gray-200 bg-white p-6 dark:border-gray-700 dark:bg-gray-900">
				<label class="mb-1.5 block text-xs font-medium text-gray-600 dark:text-gray-400">API Key</label>
				<input v-model="apiKey" type="password" autocomplete="off" :placeholder="currentProvider?.keyPlaceholder"
					class="w-full rounded-lg border border-gray-300 bg-white px-3 py-2.5 text-sm text-gray-900 placeholder-gray-400 focus:border-blue-500 focus:outline-none dark:border-gray-700 dark:bg-gray-800 dark:text-white dark:placeholder-gray-500"
					@keyup.enter="saveApiKey" />
				<p v-if="currentProvider?.keyHelp" class="mt-2 text-xs text-gray-400 dark:text-gray-500">
					{{ currentProvider.keyHelp }}
				</p>
			</div>

			<div v-if="credError"
				class="mt-3 rounded-lg bg-red-50 px-3 py-2 text-xs text-red-700 dark:bg-red-950 dark:text-red-400">
				{{ credError }}
			</div>

			<!-- API key continue button (OAuth has its own inline buttons) -->
			<div v-if="credMethod === 'apikey'" class="mt-6 flex justify-end">
				<button type="button" :disabled="!apiKey.trim() || credSaving"
					class="rounded-lg bg-blue-600 px-5 py-2.5 text-sm font-semibold text-white hover:bg-blue-500 disabled:opacity-40"
					@click="saveApiKey">
					{{ credSaving ? 'Saving…' : 'Continue →' }}
				</button>
			</div>
		</div>

		<!-- Step 3: Create agent -->
		<div v-else-if="step === 'agent'">
			<button type="button"
				class="mb-6 flex items-center gap-1.5 text-xs text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
				@click="step = 'credentials'">
				← Back
			</button>

			<div class="mb-6 text-center">
				<h2 class="text-xl font-bold text-gray-900 dark:text-white">Create your first agent</h2>
				<p class="mt-1 text-sm text-gray-500 dark:text-gray-400">An agent is an AI that can chat, run tasks, and
					remember
					things for you.</p>
			</div>

			<div class="space-y-4 rounded-xl border border-gray-200 bg-white p-6 dark:border-gray-700 dark:bg-gray-900">
				<div>
					<label class="mb-1.5 block text-xs font-medium text-gray-600 dark:text-gray-400">Agent name</label>
					<input v-model="agentName" type="text" placeholder="assistant"
						class="w-full rounded-lg border border-gray-300 bg-white px-3 py-2.5 text-sm text-gray-900 placeholder-gray-400 focus:border-blue-500 focus:outline-none dark:border-gray-700 dark:bg-gray-800 dark:text-white dark:placeholder-gray-500" />
					<p class="mt-1 text-xs text-gray-400 dark:text-gray-500">Lowercase letters, numbers, and hyphens only.</p>
				</div>
				<div>
						<label class="mb-1.5 block text-xs font-medium text-gray-600 dark:text-gray-400">Model</label>
						<div class="flex items-center gap-3">
							<select v-if="currentProvider" v-model="agentModelInput"
								class="rounded-lg border border-gray-300 bg-white px-3 py-2.5 text-sm text-gray-900 placeholder-gray-400 focus:border-blue-500 focus:outline-none dark:border-gray-700 dark:bg-gray-800 dark:text-white dark:placeholder-gray-500">
								<option :value="currentProvider.defaultModel">Default — {{ currentProvider.defaultModel }}</option>
								<option v-if="currentProvider.oauthModel" :value="currentProvider.oauthModel">OAuth — {{ currentProvider.oauthModel }}</option>
							</select>
							<input v-model="agentModelInput" type="text" placeholder="provider/model-name"
								class="flex-1 rounded-lg border border-gray-300 bg-white px-3 py-2.5 text-sm text-gray-900 placeholder-gray-400 focus:border-blue-500 focus:outline-none dark:border-gray-700 dark:bg-gray-800 dark:text-white dark:placeholder-gray-500" />
						</div>
						<p class="mt-1 text-xs text-gray-400 dark:text-gray-500">Format: <code
								class="font-mono">provider/model-name</code></p>
				</div>
				<div v-if="agentError"
					class="rounded-lg bg-red-50 px-3 py-2 text-xs text-red-700 dark:bg-red-950 dark:text-red-400">
					{{ agentError }}
				</div>
			</div>

			<div class="mt-6 flex justify-end">
				<button type="button" :disabled="!agentName.trim() || !agentModelInput.trim() || agentSaving"
					class="rounded-lg bg-blue-600 px-5 py-2.5 text-sm font-semibold text-white hover:bg-blue-500 disabled:opacity-40"
					@click="createAgent">
					{{ agentSaving ? 'Creating…' : 'Create agent →' }}
				</button>
			</div>
		</div>

		<!-- Step 4: Done -->
		<div v-else-if="step === 'done'" class="text-center">
			<div class="mb-6 flex justify-center">
				<span class="flex h-16 w-16 items-center justify-center rounded-full bg-green-100 dark:bg-green-900/40">
					<svg class="h-8 w-8 text-green-600 dark:text-green-400" fill="none" viewBox="0 0 24 24" stroke="currentColor"
						stroke-width="2">
						<path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" />
					</svg>
				</span>
			</div>
			<h2 class="mb-2 text-2xl font-bold text-gray-900 dark:text-white">You're all set!</h2>
			<p class="mb-8 text-sm text-gray-500 dark:text-gray-400">
				Agent <strong class="text-gray-900 dark:text-white">{{ agentName }}</strong> is ready to go.
				Start chatting or explore settings to add tasks and channels.
			</p>
			<div class="flex items-center justify-center gap-3">
				<router-link to="/settings/agents"
					class="rounded-lg border border-gray-200 px-4 py-2.5 text-sm font-medium text-gray-700 hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800">
					Explore settings
				</router-link>
				<router-link to="/chat"
					class="rounded-lg bg-blue-600 px-5 py-2.5 text-sm font-semibold text-white hover:bg-blue-500">
					Start chatting →
				</router-link>
			</div>
		</div>
	</div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { useMCP } from "../composables/useMCP";
import { type AppConfig, useSettingsStore } from "../stores/settings";

defineEmits<{ skip: [] }>();

interface Provider {
	id: string;
	name: string;
	emoji: string;
	description: string;
	// OAuth support
	oauth?: boolean;
	// API key fields (optional when oauth-only)
	keyPlaceholder?: string;
	keyHelp?: string;
	apiAuthKey?: string;
	// Default model when using API key
	defaultModel: string;
	// Default model when using OAuth (may differ, e.g. openai-codex)
	oauthModel?: string;
	// Credential store keys used to detect existing auth (oauth key first, then api key)
	authKeys: string[];
}

const providers: Provider[] = [
	{
		id: "anthropic",
		name: "Anthropic",
		emoji: "🤖",
		description: "Claude — great for coding & reasoning",
		oauth: true,
		keyPlaceholder: "sk-ant-...",
		keyHelp: "Find your key at console.anthropic.com → API Keys.",
		apiAuthKey: "anthropic:default",
		defaultModel: "anthropic/claude-sonnet-4-5",
		oauthModel: "anthropic/claude-sonnet-4-5",
		authKeys: ["anthropic:oauth", "anthropic:default"],
	},
	{
		id: "openai",
		name: "OpenAI",
		emoji: "🧠",
		description: "GPT-4o — versatile and capable",
		oauth: true,
		keyPlaceholder: "sk-...",
		keyHelp: "Find your key at platform.openai.com → API keys.",
		apiAuthKey: "openai:default",
		defaultModel: "openai/gpt-4o",
		oauthModel: "openai-codex/gpt-5.2",
		authKeys: ["openai:oauth", "openai:default"],
	},
	{
		id: "gemini",
		name: "Gemini",
		emoji: "✨",
		description: "Google Gemini — fast and multimodal",
		oauth: true,
		keyPlaceholder: "AIza...",
		keyHelp: "Find your key at aistudio.google.com → Get API key.",
		apiAuthKey: "gemini:default",
		defaultModel: "gemini/gemini-2.0-flash",
		oauthModel: "gemini/gemini-2.0-flash",
		authKeys: ["gemini:oauth", "gemini:default"],
	},
	{
		id: "github-copilot",
		name: "GitHub Copilot",
		emoji: "🐦",
		description: "GitHub Copilot — code-specialized models",
		oauth: true,
		keyPlaceholder: "ghp_... or personal access token",
		keyHelp:
			"Use a GitHub Personal Access Token (repo scope) or sign in via OAuth.",
		apiAuthKey: "github-copilot:default",
		defaultModel: "github-copilot/gpt-5",
		authKeys: ["github-copilot:oauth", "github-copilot:default"],
	},
];

type Step = "provider" | "credentials" | "agent" | "done";
const steps: Step[] = ["provider", "credentials", "agent", "done"];

const { callTool } = useMCP();
const settingsStore = useSettingsStore();

const step = ref<Step>("provider");
const currentStepIndex = computed(() => steps.indexOf(step.value));

const selectedProvider = ref("");
const currentProvider = computed(
	() => providers.find((p) => p.id === selectedProvider.value) ?? null,
);

// "oauth" or "apikey"
const credMethod = ref<"oauth" | "apikey">("oauth");

const apiKey = ref("");
const credSaving = ref(false);
const credError = ref("");

// Anthropic two-step OAuth state
const oauthUrl = ref("");
const oauthCode = ref("");

const copilotUserCode = ref("");
const copilotVerifyUrl = ref("");

const agentName = ref("assistant");
const agentModelInput = ref("");
const agentSaving = ref(false);
const agentError = ref("");

// Keys already stored in the credential store, populated on mount
const storedKeys = ref<string[]>([]);

onMounted(async () => {
	try {
		const raw = await callTool("auth_list");
		storedKeys.value = (JSON.parse(raw) as string[]) ?? [];
	} catch {
		// Non-fatal; wizard just won't auto-detect existing credentials
	}
});

// Returns the detected auth method for a provider (or null if none found)
function detectedAuth(p: Provider | undefined): "oauth" | "apikey" | null {
	if (!p) return null;
	if (p.oauth && storedKeys.value.includes(p.authKeys[0])) return "oauth";
	if (p.apiAuthKey && storedKeys.value.includes(p.apiAuthKey)) return "apikey";
	return null;
}

function selectProvider(id: string) {
	selectedProvider.value = id;
	const p = providers.find((x) => x.id === id);
	if (!p) return;
	const existing = detectedAuth(p);
	if (existing) {
		// Already authenticated — set method + model and skip to agent creation
		credMethod.value = existing;
		agentModelInput.value =
			existing === "oauth" && p.oauthModel ? p.oauthModel : p.defaultModel;
		step.value = "agent";
		return;
	}
	// Default to OAuth if the provider supports it, otherwise API key
	credMethod.value = p.oauth ? "oauth" : "apikey";
	// Reset state
	apiKey.value = "";
	oauthUrl.value = "";
	oauthCode.value = "";
	credError.value = "";
	agentModelInput.value = "";
}

// Pre-fill model when the method is chosen
watch([currentProvider, credMethod], ([p, method]) => {
	if (!p) return;
	agentModelInput.value =
		method === "oauth" && p.oauthModel ? p.oauthModel : p.defaultModel;
});

// ── API key ───────────────────────────────────────────────────────────────────

async function saveApiKey() {
	if (!apiKey.value.trim() || !currentProvider.value?.apiAuthKey) return;
	credSaving.value = true;
	credError.value = "";
	try {
		await callTool("auth_set", {
			name: currentProvider.value.apiAuthKey,
			value: apiKey.value.trim(),
		});
		step.value = "agent";
	} catch (e) {
		credError.value = e instanceof Error ? e.message : String(e);
	} finally {
		credSaving.value = false;
	}
}

// ── Anthropic OAuth (two-step) ────────────────────────────────────────────────

async function startAnthropicOAuth() {
	credSaving.value = true;
	credError.value = "";
	try {
		const raw = await callTool("auth_login_anthropic");
		const parsed = JSON.parse(raw) as { url?: string };
		oauthUrl.value = parsed.url ?? "";
	} catch (e) {
		credError.value = e instanceof Error ? e.message : String(e);
	} finally {
		credSaving.value = false;
	}
}

async function completeAnthropicOAuth() {
	if (!oauthCode.value.trim()) return;
	credSaving.value = true;
	credError.value = "";
	try {
		await callTool("auth_login_anthropic_complete", {
			code: oauthCode.value.trim(),
		});
		step.value = "agent";
	} catch (e) {
		credError.value = e instanceof Error ? e.message : String(e);
	} finally {
		credSaving.value = false;
	}
}

// ── OpenAI OAuth (blocking browser redirect) ──────────────────────────────────

async function startOpenAIOAuth() {
	credSaving.value = true;
	credError.value = "";
	try {
		// This call blocks until the browser redirect completes (up to 5 min)
		await callTool("auth_login_openai");
		step.value = "agent";
	} catch (e) {
		credError.value = e instanceof Error ? e.message : String(e);
		credSaving.value = false;
	}
	// Don't clear credSaving on success — the step transition handles it
}

// ── GitHub Copilot OAuth (device flow) ────────────────────────────────────────

async function startCopilotOAuth() {
	credSaving.value = true;
	credError.value = "";
	try {
		const raw = await callTool("auth_login_github_copilot");
		const parsed = JSON.parse(raw) as {
			user_code?: string;
			verification_uri?: string;
		};
		copilotUserCode.value = parsed.user_code ?? "";
		copilotVerifyUrl.value = parsed.verification_uri ?? "";
	} catch (e) {
		credError.value = e instanceof Error ? e.message : String(e);
	} finally {
		credSaving.value = false;
	}
}

async function completeCopilotOAuth() {
	credSaving.value = true;
	credError.value = "";
	try {
		await callTool("auth_login_github_copilot_complete");
		step.value = "agent";
	} catch (e) {
		credError.value = e instanceof Error ? e.message : String(e);
	} finally {
		credSaving.value = false;
	}
}

// ── Gemini OAuth (blocking browser redirect) ──────────────────────────────────

async function startGeminiOAuth() {
	credSaving.value = true;
	credError.value = "";
	try {
		await callTool("auth_login_gemini");
		step.value = "agent";
	} catch (e) {
		credError.value = e instanceof Error ? e.message : String(e);
		credSaving.value = false;
	}
}

// ── Create agent ──────────────────────────────────────────────────────────────

async function createAgent() {
	if (!agentName.value.trim() || !agentModelInput.value.trim()) return;
	agentSaving.value = true;
	agentError.value = "";
	try {
		await settingsStore.fetchConfig();
		const base = settingsStore.config
			? (JSON.parse(JSON.stringify(settingsStore.config)) as AppConfig)
			: emptyConfig();

		const name = agentName.value.trim();
		if (!base.agents.find((a) => a.name === name)) {
			base.agents.push({
				name,
				model: agentModelInput.value.trim(),
				memory: "",
				fallbacks: [],
				channels: [],
				tasks: [],
			});
		}

		await settingsStore.saveConfig(base);
		step.value = "done";
	} catch (e) {
		agentError.value = e instanceof Error ? e.message : String(e);
	} finally {
		agentSaving.value = false;
	}
}

function emptyConfig(): AppConfig {
	return {
		server: {
			port: 0,
			tls: { cert: "", key: "" },
			external_access: false,
			no_tls: false,
		},
		agents: [],
		models: { providers: {}, defaults: { model: "", fallbacks: [] } },
		browser: { binary: "", cdp_port: 0, reuse_tabs: true },
		search: { web: { brave_api_key: "" } },
		scheduler: { concurrency: "auto" },
		skills: {},
	};
}
</script>
