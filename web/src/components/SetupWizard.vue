<template>
	<div class="mx-auto max-w-2xl px-4 py-12">
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

		<div v-if="step === 'provider'" class="text-center">
			<h1 class="mb-2 text-2xl font-bold text-gray-900 dark:text-white">Welcome to Aviary</h1>
			<p class="mb-8 text-sm text-gray-500 dark:text-gray-400">
				Let's get you set up in under a minute. Which AI provider would you like to use?
			</p>

			<div class="grid gap-3 sm:grid-cols-3">
				<button v-for="p in providers" :key="p.id" type="button" :class="[
					'flex flex-col items-center gap-3 rounded-xl border-2 p-5 transition-all hover:shadow-md',
					selectedProvider === p.id
						? 'border-blue-500 bg-blue-50 dark:bg-blue-950/30'
						: 'border-gray-200 bg-white hover:border-gray-300 dark:border-gray-700 dark:bg-gray-900 dark:hover:border-gray-600',
				]" @click="selectProvider(p.id)">
					<span class="text-3xl">{{ p.emoji }}</span>
					<div>
						<p class="font-semibold text-gray-900 dark:text-white">{{ p.label }}</p>
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
					Continue
				</button>
			</div>
		</div>

		<div v-else-if="step === 'credentials'">
			<button type="button"
				class="mb-6 flex items-center gap-1.5 text-xs text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
				@click="step = 'provider'">
				<- Back </button>

					<div class="mb-6 text-center">
						<span class="mb-3 inline-block text-4xl">{{ currentProvider?.emoji }}</span>
						<h2 class="text-xl font-bold text-gray-900 dark:text-white">Connect {{ currentProvider?.label }}</h2>
					</div>

					<div v-if="currentProvider?.hasOAuth"
						class="mb-5 flex rounded-lg border border-gray-200 bg-gray-50 p-1 dark:border-gray-700 dark:bg-gray-800/50">
						<button type="button" :class="[
							'flex-1 rounded-md py-1.5 text-xs font-semibold transition-colors',
							credMethod === 'oauth'
								? 'bg-white text-gray-900 shadow-sm dark:bg-gray-900 dark:text-white'
								: 'text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200',
						]" @click="credMethod = 'oauth'">
							Sign in with {{ currentProvider.label }}
						</button>
						<button type="button" :class="[
							'flex-1 rounded-md py-1.5 text-xs font-semibold transition-colors',
							credMethod === 'apikey'
								? 'bg-white text-gray-900 shadow-sm dark:bg-gray-900 dark:text-white'
								: 'text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200',
						]" @click="credMethod = 'apikey'">
							API key
						</button>
					</div>

					<div v-if="credMethod === 'oauth'"
						class="rounded-xl border border-gray-200 bg-white p-6 dark:border-gray-700 dark:bg-gray-900">
						<template v-if="currentProvider?.id === 'anthropic'">
							<div v-if="!oauthUrl">
								<p class="mb-4 text-sm text-gray-600 dark:text-gray-400">
									Sign in with your Claude Pro or Claude Max account - no API key needed.
									We'll open Anthropic's authorization page for you.
								</p>
								<button type="button" :disabled="credSaving"
									class="w-full rounded-lg bg-orange-500 px-4 py-2.5 text-sm font-semibold text-white hover:bg-orange-400 disabled:opacity-40"
									@click="startAnthropicOAuth">
									{{ credSaving ? "Opening..." : "Sign in with Anthropic" }}
								</button>
							</div>
							<div v-else class="space-y-4">
								<p class="text-sm text-gray-700 dark:text-gray-300">
									Open the authorization URL below, sign in there, then paste the code that Anthropic shows you.
								</p>
								<div
									class="rounded-lg border border-gray-200 bg-gray-50 p-3 text-left dark:border-gray-700 dark:bg-gray-800">
									<p class="text-xs font-medium text-gray-700 dark:text-gray-300">Authorization page</p>
									<a :href="oauthUrl" target="_blank" rel="noreferrer"
										class="mt-1 block break-all text-xs text-blue-600 hover:underline dark:text-blue-400">
										{{ oauthUrl }}
									</a>
								</div>
								<div>
									<label class="mb-1.5 block text-xs font-medium text-gray-600 dark:text-gray-400">Authorization
										code</label>
									<input v-model="oauthCode" type="text" placeholder="Paste code here..."
										class="w-full rounded-lg border border-gray-300 bg-white px-3 py-2.5 text-sm text-gray-900 placeholder-gray-400 focus:border-blue-500 focus:outline-none dark:border-gray-700 dark:bg-gray-800 dark:text-white dark:placeholder-gray-500"
										@keyup.enter="completeAnthropicOAuth" />
								</div>
								<button type="button" :disabled="!oauthCode.trim() || credSaving"
									class="w-full rounded-lg bg-orange-500 px-4 py-2.5 text-sm font-semibold text-white hover:bg-orange-400 disabled:opacity-40"
									@click="completeAnthropicOAuth">
									{{ credSaving ? "Verifying..." : "Complete sign-in" }}
								</button>
							</div>
						</template>

						<template v-else-if="currentProvider?.id === 'openai'">
							<div v-if="!openAIUrl">
								<p class="mb-4 text-sm text-gray-600 dark:text-gray-400">
									Sign in with your ChatGPT Plus or Pro account. We'll open OpenAI's consent page and also show the full
									URL here in case the browser opens on a different machine.
								</p>
								<p class="mb-4 text-xs text-gray-500 dark:text-gray-400">
									This uses the <code class="font-mono">openai-codex</code> provider and requires a ChatGPT Plus or Pro
									subscription.
								</p>
								<button type="button" :disabled="credSaving"
									class="w-full rounded-lg bg-green-600 px-4 py-2.5 text-sm font-semibold text-white hover:bg-green-500 disabled:opacity-40"
									@click="startOpenAIOAuth">
									{{ credSaving ? "Opening..." : "Sign in with OpenAI Codex" }}
								</button>
							</div>
							<div v-else class="space-y-4">
								<p class="text-sm text-gray-700 dark:text-gray-300">
									Open the authorization URL below on a machine that can reach the callback URL, finish sign-in there,
									then come back here and click complete.
								</p>
								<div
									class="rounded-lg border border-gray-200 bg-gray-50 p-3 text-left dark:border-gray-700 dark:bg-gray-800">
									<p class="text-xs font-medium text-gray-700 dark:text-gray-300">Authorization page</p>
									<a :href="openAIUrl" target="_blank" rel="noreferrer"
										class="mt-1 block break-all text-xs text-blue-600 hover:underline dark:text-blue-400">
										{{ openAIUrl }}
									</a>
								</div>
								<div
									class="rounded-lg border border-gray-200 bg-gray-50 p-3 text-left dark:border-gray-700 dark:bg-gray-800">
									<p class="text-xs font-medium text-gray-700 dark:text-gray-300">Callback URL</p>
									<p class="mt-1 break-all font-mono text-[11px] text-gray-700 dark:text-gray-300">
										{{ openAICallbackUrl }}</p>
								</div>
								<p
									:class="openAITimedOut ? 'text-xs font-medium text-red-600 dark:text-red-400' : 'text-xs text-gray-500 dark:text-gray-400'">
									{{ openAITimedOut ? "This OpenAI Codex callback timed out. Start over to reopen port 1455." : `Callback expires in ${formatCountdown(openAIRemainingSeconds)}.` }}
								</p>
								<button type="button" :disabled="credSaving || openAITimedOut"
									class="w-full rounded-lg bg-green-600 px-4 py-2.5 text-sm font-semibold text-white hover:bg-green-500 disabled:opacity-40"
									@click="completeOpenAIOAuth">
									{{ openAITimedOut ? "Timed out" : credSaving ? "Completing..." : "I've authorized - Continue" }}
								</button>
							</div>
						</template>

						<template v-else-if="currentProvider?.id === 'gemini'">
							<div v-if="!geminiUrl">
								<p class="mb-4 text-sm text-gray-600 dark:text-gray-400">
									Sign in with your Google account. We'll open Google's consent page and also show the full URL here in
									case the browser opens on a different machine.
								</p>
								<button type="button" :disabled="credSaving"
									class="w-full rounded-lg bg-blue-500 px-4 py-2.5 text-sm font-semibold text-white hover:bg-blue-400 disabled:opacity-40"
									@click="startGeminiOAuth">
									{{ credSaving ? "Opening..." : "Sign in with Gemini" }}
								</button>
							</div>
							<div v-else class="space-y-4">
								<p class="text-sm text-gray-700 dark:text-gray-300">
									Open the authorization URL below on a machine that can reach the callback URL, finish sign-in there,
									then come back here and click complete.
								</p>
								<div
									class="rounded-lg border border-gray-200 bg-gray-50 p-3 text-left dark:border-gray-700 dark:bg-gray-800">
									<p class="text-xs font-medium text-gray-700 dark:text-gray-300">Authorization page</p>
									<a :href="geminiUrl" target="_blank" rel="noreferrer"
										class="mt-1 block break-all text-xs text-blue-600 hover:underline dark:text-blue-400">
										{{ geminiUrl }}
									</a>
								</div>
								<div
									class="rounded-lg border border-gray-200 bg-gray-50 p-3 text-left dark:border-gray-700 dark:bg-gray-800">
									<p class="text-xs font-medium text-gray-700 dark:text-gray-300">Callback URL</p>
									<p class="mt-1 break-all font-mono text-[11px] text-gray-700 dark:text-gray-300">
										{{ geminiCallbackUrl }}</p>
								</div>
								<p
									:class="geminiTimedOut ? 'text-xs font-medium text-red-600 dark:text-red-400' : 'text-xs text-gray-500 dark:text-gray-400'">
									{{ geminiTimedOut ? "This Gemini callback timed out. Start over to reopen port 45289." : `Callback expires in ${formatCountdown(geminiRemainingSeconds)}.` }}
								</p>
								<button type="button" :disabled="credSaving || geminiTimedOut"
									class="w-full rounded-lg bg-blue-500 px-4 py-2.5 text-sm font-semibold text-white hover:bg-blue-400 disabled:opacity-40"
									@click="completeGeminiOAuth">
									{{ geminiTimedOut ? "Timed out" : credSaving ? "Completing..." : "I've authorized - Continue" }}
								</button>
							</div>
						</template>

						<template v-else-if="currentProvider?.id === 'github-copilot'">
							<div v-if="!copilotUserCode">
								<p class="mb-4 text-sm text-gray-600 dark:text-gray-400">
									Sign in with your GitHub account that has Copilot access.
									No browser redirect is needed - we'll show you a short code to enter on GitHub.
								</p>
								<button type="button" :disabled="credSaving"
									class="w-full rounded-lg bg-gray-900 px-4 py-2.5 text-sm font-semibold text-white hover:bg-gray-700 disabled:opacity-40 dark:bg-gray-700 dark:hover:bg-gray-600"
									@click="startCopilotOAuth">
									{{ credSaving ? "Requesting device code..." : "Sign in with GitHub" }}
								</button>
							</div>
							<div v-else class="space-y-4">
								<p class="text-sm text-gray-700 dark:text-gray-300">
									Visit
									<a :href="copilotVerifyUrl" target="_blank" rel="noreferrer"
										class="font-medium text-blue-600 hover:underline dark:text-blue-400">
										{{ copilotVerifyUrl }}
									</a>
									and enter this code:
								</p>
								<div
									class="rounded-lg border border-gray-200 bg-gray-50 p-3 text-left dark:border-gray-700 dark:bg-gray-800">
									<p class="mb-2 text-xs font-medium text-gray-700 dark:text-gray-300">Verification page</p>
									<a :href="copilotVerifyUrl" target="_blank" rel="noreferrer"
										class="block break-all text-xs text-blue-600 hover:underline dark:text-blue-400">
										{{ copilotVerifyUrl }}
									</a>
								</div>
								<div
									class="flex items-center justify-center rounded-lg border border-gray-200 bg-gray-50 py-4 dark:border-gray-700 dark:bg-gray-800">
									<span
										class="font-mono text-2xl font-bold tracking-widest text-gray-900 dark:text-white">{{ copilotUserCode }}</span>
								</div>
								<button type="button" :disabled="credSaving"
									class="w-full rounded-lg bg-gray-900 px-4 py-2.5 text-sm font-semibold text-white hover:bg-gray-700 disabled:opacity-40 dark:bg-gray-700 dark:hover:bg-gray-600"
									@click="completeCopilotOAuth">
									{{ credSaving ? "Waiting for authorization..." : "I've authorized - Continue" }}
								</button>
							</div>
						</template>
					</div>

					<div v-else class="rounded-xl border border-gray-200 bg-white p-6 dark:border-gray-700 dark:bg-gray-900">
						<div v-if="currentProvider?.requiresBaseURI" class="mb-4">
							<label class="mb-1.5 block text-xs font-medium text-gray-600 dark:text-gray-400">Base URI</label>
							<input v-model="baseURI" type="text" autocomplete="off" :placeholder="currentProvider?.baseURIPlaceholder"
								class="w-full rounded-lg border border-gray-300 bg-white px-3 py-2.5 text-sm text-gray-900 placeholder-gray-400 focus:border-blue-500 focus:outline-none dark:border-gray-700 dark:bg-gray-800 dark:text-white dark:placeholder-gray-500"
								@keyup.enter="saveApiKey" />
							<p class="mt-2 text-xs text-gray-400 dark:text-gray-500">
								Point this at your vLLM host and port. Aviary will use its OpenAI-compatible API and append <code
									class="font-mono">/v1</code> automatically when needed.
							</p>
						</div>
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

					<div v-if="credMethod === 'apikey'" class="mt-6 flex justify-end">
						<button type="button" :disabled="credentialsContinueDisabled"
							class="rounded-lg bg-blue-600 px-5 py-2.5 text-sm font-semibold text-white hover:bg-blue-500 disabled:opacity-40"
							@click="saveApiKey">
							{{ credSaving ? "Saving..." : "Continue" }}
						</button>
					</div>
		</div>

		<div v-else-if="step === 'agent'">
			<button type="button"
				class="mb-6 flex items-center gap-1.5 text-xs text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
				@click="step = 'credentials'">
				<- Back </button>

					<div class="mb-6 text-center">
						<h2 class="text-xl font-bold text-gray-900 dark:text-white">Create your first agent</h2>
						<p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
							An agent is an AI that can chat, run tasks, and remember things for you.
						</p>
					</div>

					<div class="space-y-4 rounded-xl border border-gray-200 bg-white p-6 dark:border-gray-700 dark:bg-gray-900">
						<div>
							<label class="mb-1.5 block text-xs font-medium text-gray-600 dark:text-gray-400">Agent name</label>
							<input v-model="agentName" type="text" placeholder="Aviary"
								class="w-full rounded-lg border border-gray-300 bg-white px-3 py-2.5 text-sm text-gray-900 placeholder-gray-400 focus:border-blue-500 focus:outline-none dark:border-gray-700 dark:bg-gray-800 dark:text-white dark:placeholder-gray-500" />
							<p class="mt-1 text-xs text-gray-400 dark:text-gray-500">Lowercase letters, numbers, and hyphens only.</p>
						</div>
						<div>
							<label class="mb-1.5 block text-xs font-medium text-gray-600 dark:text-gray-400">Model</label>
							<ModelSelector v-model="agentModelInput" :options="currentProviderModelOptions"
								placeholder="Select a model..." />
							<div v-if="currentProvider?.id === 'vllm'" class="mt-2 space-y-2">
								<div class="flex items-center justify-between gap-3">
									<p class="text-xs text-gray-400 dark:text-gray-500">
										{{ vllmModelsLoading ? "Loading models from vLLM..." : "Select a model discovered from the endpoint." }}
									</p>
									<button type="button"
										class="text-xs font-medium text-blue-600 hover:text-blue-500 disabled:opacity-40 dark:text-blue-400"
										:disabled="vllmModelsLoading || !baseURI.trim()" @click="refreshVLLMModels">
										{{ vllmModelsLoading ? "Refreshing..." : "Refresh models" }}
									</button>
								</div>
							</div>
							<p class="mt-1 text-xs text-gray-400 dark:text-gray-500">Format: <code
									class="font-mono">provider/model-name</code></p>
						</div>
						<div>
							<label class="mb-1.5 block text-xs font-medium text-gray-600 dark:text-gray-400">Fallbacks</label>
							<ModelSelector v-model="agentFallbacks" :options="fallbackModelOptions"
								placeholder="Optional fallback models..." multiple />
							<p class="mt-1 text-xs text-gray-400 dark:text-gray-500">
								Optional. Aviary will try these in order if the primary model is unavailable.
							</p>
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
							{{ agentSaving ? "Creating..." : "Create agent" }}
						</button>
					</div>
		</div>

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
					Start chatting
				</router-link>
			</div>
		</div>
	</div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { useAvailableModels } from "../composables/useAvailableModels";
import { useMCP } from "../composables/useMCP";
import {
	KNOWN_PROVIDERS,
	type KnownProvider,
	useProviderAuth,
} from "../composables/useProviderAuth";
import { type AppConfig, useSettingsStore } from "../stores/settings";
import ModelSelector from "./ModelSelector.vue";

defineEmits<{ skip: [] }>();

type Provider = KnownProvider;

const providers: Provider[] = KNOWN_PROVIDERS.filter((provider) =>
	["anthropic", "openai-codex", "google", "github-copilot", "vllm"].includes(
		provider.id,
	),
);

type Step = "provider" | "credentials" | "agent" | "done";
const steps: Step[] = ["provider", "credentials", "agent", "done"];

const { callTool } = useMCP();
const { availableModelOptions, refreshCredentials } = useAvailableModels();
const settingsStore = useSettingsStore();
const {
	anthropicUrl: oauthUrl,
	anthropicCode: oauthCode,
	openAIUrl,
	openAICallbackUrl,
	openAIRemainingSeconds,
	openAITimedOut,
	geminiUrl,
	geminiCallbackUrl,
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
} = useProviderAuth(callTool);

const step = ref<Step>("provider");
const currentStepIndex = computed(() => steps.indexOf(step.value));
const selectedProvider = ref("");
const currentProvider = computed(
	() => providers.find((p) => p.id === selectedProvider.value) ?? null,
);
const currentProviderModelOptions = computed(() => {
	const provider = currentProvider.value;
	if (!provider) return [];
	if (provider.id === "vllm") {
		return vllmModelOptions.value;
	}
	const allowedProviders =
		(credMethod.value === "oauth"
			? (provider.oauthProviders ?? provider.defaultProviders)
			: provider.defaultProviders) ?? [];
	const options = availableModelOptions.value.filter((model) =>
		allowedProviders.some((prefix) => model.startsWith(`${prefix}/`)),
	);
	return options.length
		? options
		: [provider.defaultModel, provider.oauthModel].filter(
			(model): model is string => Boolean(model),
		);
});
const fallbackModelOptions = computed(() =>
	currentProviderModelOptions.value.filter(
		(model) => model !== agentModelInput.value.trim(),
	),
);
const credMethod = ref<"oauth" | "apikey">("oauth");
const apiKey = ref("");
const baseURI = ref("");
const credentialsContinueDisabled = computed(() => {
	if (credSaving.value) return true;
	if (!currentProvider.value) return true;
	if (currentProvider.value.requiresBaseURI) {
		return !baseURI.value.trim();
	}
	return !apiKey.value.trim();
});
const credSaving = ref(false);
const credError = ref("");
const agentName = ref("Aviary");
const agentModelInput = ref("");
const agentFallbacks = ref<string[]>([]);
const vllmModelOptions = ref<string[]>([]);
const vllmModelsLoading = ref(false);
const agentSaving = ref(false);
const agentError = ref("");
const storedKeys = ref<string[]>([]);

function formatCountdown(seconds: number | null): string {
	if (seconds == null) return "";
	const mins = Math.floor(seconds / 60);
	const secs = seconds % 60;
	return `${mins}:${String(secs).padStart(2, "0")}`;
}

onMounted(async () => {
	await refreshStoredKeys();
	await refreshCredentials();
	await loadVLLMProviderConfig();
});

async function refreshStoredKeys() {
	try {
		const raw = await callTool("auth_list");
		storedKeys.value = (JSON.parse(raw) as string[]) ?? [];
	} catch {
		// Non-fatal; wizard just won't auto-detect existing credentials.
	}
}

function detectedAuth(p: Provider | undefined): "oauth" | "apikey" | null {
	if (!p) return null;
	if (p.hasOAuth && p.authKeys?.[0] && storedKeys.value.includes(p.authKeys[0]))
		return "oauth";
	if (p.apiAuthKey && storedKeys.value.includes(p.apiAuthKey)) return "apikey";
	return null;
}

function defaultModelFor(p: Provider, method: "oauth" | "apikey"): string {
	return method === "oauth" && p.oauthModel
		? p.oauthModel
		: (p.defaultModel ?? "");
}

async function loadVLLMProviderConfig() {
	try {
		await settingsStore.fetchConfig();
		baseURI.value =
			settingsStore.config?.models.providers?.vllm?.base_uri?.trim() ?? "";
	} catch {
		// best effort
	}
}

async function refreshVLLMModels() {
	if (!baseURI.value.trim()) return;
	vllmModelsLoading.value = true;
	credError.value = "";
	try {
		const raw = await callTool("models_list", {
			provider: "vllm",
			base_uri: baseURI.value.trim(),
			auth: apiKey.value.trim() || undefined,
		});
		vllmModelOptions.value = (JSON.parse(raw) as string[]) ?? [];
		if (!agentModelInput.value.trim() && vllmModelOptions.value.length > 0) {
			agentModelInput.value = vllmModelOptions.value[0];
		}
	} catch (e) {
		credError.value = e instanceof Error ? e.message : String(e);
	} finally {
		vllmModelsLoading.value = false;
	}
}

function selectProvider(id: string) {
	selectedProvider.value = id;
	const p = providers.find((x) => x.id === id);
	if (!p) return;
	if (p.id === "vllm") {
		void loadVLLMProviderConfig();
	}
	const existing = detectedAuth(p);
	if (existing && (!p.requiresBaseURI || baseURI.value.trim())) {
		credMethod.value = existing;
		agentModelInput.value = defaultModelFor(p, existing);
		if (p.id === "vllm") {
			void refreshVLLMModels();
		}
		step.value = "agent";
		return;
	}
	credMethod.value = p.hasOAuth ? "oauth" : "apikey";
	apiKey.value = "";
	if (p.id !== "vllm") {
		baseURI.value = "";
	}
	clearOAuthState();
	credError.value = "";
	agentModelInput.value = "";
}

watch([currentProvider, credMethod], ([p, method]) => {
	if (!p) return;
	agentModelInput.value = defaultModelFor(p, method);
});

watch(baseURI, () => {
	if (currentProvider.value?.id === "vllm") {
		vllmModelOptions.value = [];
	}
});

watch(agentModelInput, (model) => {
	const trimmed = model.trim();
	agentFallbacks.value = agentFallbacks.value.filter((fb) => fb !== trimmed);
});

async function saveApiKey() {
	if (!currentProvider.value) return;
	if (currentProvider.value.requiresBaseURI && !baseURI.value.trim()) {
		credError.value = "base URI is required";
		return;
	}
	if (
		!currentProvider.value.requiresBaseURI &&
		(!apiKey.value.trim() || !currentProvider.value.apiAuthKey)
	) {
		return;
	}
	credSaving.value = true;
	credError.value = "";
	try {
		await settingsStore.fetchConfig();
		const base = settingsStore.config
			? (JSON.parse(JSON.stringify(settingsStore.config)) as AppConfig)
			: emptyConfig();
		if (currentProvider.value.id === "vllm") {
			base.models.providers.vllm = {
				...(base.models.providers.vllm ?? { auth: "" }),
				auth:
					apiKey.value.trim() && currentProvider.value.apiAuthKey
						? `auth:${currentProvider.value.apiAuthKey}`
						: "",
				base_uri: baseURI.value.trim(),
			};
			await settingsStore.saveConfig(base);
			if (apiKey.value.trim() && currentProvider.value.apiAuthKey) {
				await callTool("auth_set", {
					name: currentProvider.value.apiAuthKey,
					value: apiKey.value.trim(),
				});
			}
			await refreshVLLMModels();
		} else if (currentProvider.value.apiAuthKey) {
			await callTool("auth_set", {
				name: currentProvider.value.apiAuthKey,
				value: apiKey.value.trim(),
			});
		}
		await refreshStoredKeys();
		await refreshCredentials();
		step.value = "agent";
	} catch (e) {
		credError.value = e instanceof Error ? e.message : String(e);
	} finally {
		credSaving.value = false;
	}
}

async function startAnthropicOAuth() {
	credSaving.value = true;
	credError.value = "";
	try {
		await startAnthropic();
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
		await completeAnthropic();
		clearOAuthState();
		await refreshStoredKeys();
		await refreshCredentials();
		step.value = "agent";
	} catch (e) {
		credError.value = e instanceof Error ? e.message : String(e);
	} finally {
		credSaving.value = false;
	}
}

async function startOpenAIOAuth() {
	credSaving.value = true;
	credError.value = "";
	try {
		await startOpenAI();
	} catch (e) {
		credError.value = e instanceof Error ? e.message : String(e);
	} finally {
		credSaving.value = false;
	}
}

async function completeOpenAIOAuth() {
	credSaving.value = true;
	credError.value = "";
	try {
		await completeOpenAI();
		clearOAuthState();
		await refreshStoredKeys();
		await refreshCredentials();
		step.value = "agent";
	} catch (e) {
		credError.value = e instanceof Error ? e.message : String(e);
	} finally {
		credSaving.value = false;
	}
}

async function startGeminiOAuth() {
	credSaving.value = true;
	credError.value = "";
	try {
		await startGemini();
	} catch (e) {
		credError.value = e instanceof Error ? e.message : String(e);
	} finally {
		credSaving.value = false;
	}
}

async function completeGeminiOAuth() {
	credSaving.value = true;
	credError.value = "";
	try {
		await completeGemini();
		clearOAuthState();
		await refreshStoredKeys();
		await refreshCredentials();
		step.value = "agent";
	} catch (e) {
		credError.value = e instanceof Error ? e.message : String(e);
	} finally {
		credSaving.value = false;
	}
}

async function startCopilotOAuth() {
	credSaving.value = true;
	credError.value = "";
	try {
		await startCopilot();
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
		await completeCopilot();
		clearOAuthState();
		await refreshStoredKeys();
		await refreshCredentials();
		step.value = "agent";
	} catch (e) {
		credError.value = e instanceof Error ? e.message : String(e);
	} finally {
		credSaving.value = false;
	}
}

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
			const primaryModel = agentModelInput.value.trim();
			const fallbacks = agentFallbacks.value
				.map((model) => model.trim())
				.filter((model) => model && model !== primaryModel);
			base.agents.push({
				name,
				model: primaryModel,
				memory: "",
				fallbacks,
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
