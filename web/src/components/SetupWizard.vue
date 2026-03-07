<template>
	<div class="mx-auto max-w-2xl px-4 py-12">
		<!-- Progress dots -->
		<div class="mb-10 flex items-center justify-center gap-2">
			<div
				v-for="(s, i) in steps"
				:key="s"
				class="flex items-center gap-2"
			>
				<div
					:class="[
						'flex h-7 w-7 items-center justify-center rounded-full text-xs font-semibold transition-colors',
						currentStepIndex > i
							? 'bg-blue-600 text-white'
							: currentStepIndex === i
								? 'bg-blue-600 text-white ring-4 ring-blue-100 dark:ring-blue-900/40'
								: 'bg-gray-200 text-gray-500 dark:bg-gray-700 dark:text-gray-400',
					]"
				>
					<svg v-if="currentStepIndex > i" class="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3">
						<path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" />
					</svg>
					<span v-else>{{ i + 1 }}</span>
				</div>
				<div v-if="i < steps.length - 1" class="h-px w-8 bg-gray-200 dark:bg-gray-700" />
			</div>
		</div>

		<!-- Step: provider -->
		<div v-if="step === 'provider'" class="text-center">
			<h1 class="mb-2 text-2xl font-bold text-gray-900 dark:text-white">Welcome to Aviary</h1>
			<p class="mb-8 text-sm text-gray-500 dark:text-gray-400">Let's get you set up in under a minute. Which AI provider would you like to use?</p>

			<div class="grid gap-3 sm:grid-cols-3">
				<button
					v-for="p in providers"
					:key="p.id"
					type="button"
					:class="[
						'flex flex-col items-center gap-3 rounded-xl border-2 p-5 transition-all hover:shadow-md',
						selectedProvider === p.id
							? 'border-blue-500 bg-blue-50 dark:bg-blue-950/30'
							: 'border-gray-200 bg-white hover:border-gray-300 dark:border-gray-700 dark:bg-gray-900 dark:hover:border-gray-600',
					]"
					@click="selectedProvider = p.id"
				>
					<span class="text-3xl">{{ p.emoji }}</span>
					<div>
						<p class="font-semibold text-gray-900 dark:text-white">{{ p.name }}</p>
						<p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">{{ p.description }}</p>
					</div>
				</button>
			</div>

			<div class="mt-8 flex items-center justify-between">
				<button type="button" class="text-xs text-gray-400 hover:text-gray-600 dark:hover:text-gray-300" @click="$emit('skip')">
					Skip for now
				</button>
				<button
					type="button"
					:disabled="!selectedProvider"
					class="rounded-lg bg-blue-600 px-5 py-2.5 text-sm font-semibold text-white hover:bg-blue-500 disabled:opacity-40"
					@click="step = 'credentials'"
				>
					Continue →
				</button>
			</div>
		</div>

		<!-- Step: credentials -->
		<div v-else-if="step === 'credentials'">
			<button type="button" class="mb-6 flex items-center gap-1.5 text-xs text-gray-400 hover:text-gray-600 dark:hover:text-gray-300" @click="step = 'provider'">
				← Back
			</button>

			<div class="mb-6 text-center">
				<span class="mb-3 inline-block text-4xl">{{ currentProvider?.emoji }}</span>
				<h2 class="text-xl font-bold text-gray-900 dark:text-white">Connect {{ currentProvider?.name }}</h2>
				<p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ currentProvider?.credentialHint }}</p>
			</div>

			<div class="rounded-xl border border-gray-200 bg-white p-6 dark:border-gray-700 dark:bg-gray-900">
				<label class="mb-1.5 block text-xs font-medium text-gray-600 dark:text-gray-400">API Key</label>
				<input
					v-model="apiKey"
					type="password"
					autocomplete="off"
					:placeholder="currentProvider?.keyPlaceholder"
					class="w-full rounded-lg border border-gray-300 bg-white px-3 py-2.5 text-sm text-gray-900 placeholder-gray-400 focus:border-blue-500 focus:outline-none dark:border-gray-700 dark:bg-gray-800 dark:text-white dark:placeholder-gray-500"
					@keyup.enter="saveCredential"
				/>
				<p v-if="currentProvider?.keyHelp" class="mt-2 text-xs text-gray-400 dark:text-gray-500">
					{{ currentProvider.keyHelp }}
				</p>
				<div v-if="credError" class="mt-3 rounded-lg bg-red-50 px-3 py-2 text-xs text-red-700 dark:bg-red-950 dark:text-red-400">
					{{ credError }}
				</div>
			</div>

			<div class="mt-6 flex justify-end">
				<button
					type="button"
					:disabled="!apiKey.trim() || credSaving"
					class="rounded-lg bg-blue-600 px-5 py-2.5 text-sm font-semibold text-white hover:bg-blue-500 disabled:opacity-40"
					@click="saveCredential"
				>
					{{ credSaving ? 'Saving…' : 'Continue →' }}
				</button>
			</div>
		</div>

		<!-- Step: agent -->
		<div v-else-if="step === 'agent'">
			<button type="button" class="mb-6 flex items-center gap-1.5 text-xs text-gray-400 hover:text-gray-600 dark:hover:text-gray-300" @click="step = 'credentials'">
				← Back
			</button>

			<div class="mb-6 text-center">
				<h2 class="text-xl font-bold text-gray-900 dark:text-white">Create your first agent</h2>
				<p class="mt-1 text-sm text-gray-500 dark:text-gray-400">An agent is an AI that can chat, run tasks, and remember things for you.</p>
			</div>

			<div class="space-y-4 rounded-xl border border-gray-200 bg-white p-6 dark:border-gray-700 dark:bg-gray-900">
				<div>
					<label class="mb-1.5 block text-xs font-medium text-gray-600 dark:text-gray-400">Agent name</label>
					<input
						v-model="agentName"
						type="text"
						placeholder="assistant"
						class="w-full rounded-lg border border-gray-300 bg-white px-3 py-2.5 text-sm text-gray-900 placeholder-gray-400 focus:border-blue-500 focus:outline-none dark:border-gray-700 dark:bg-gray-800 dark:text-white dark:placeholder-gray-500"
					/>
					<p class="mt-1 text-xs text-gray-400 dark:text-gray-500">Lowercase letters, numbers, and hyphens only.</p>
				</div>
				<div>
					<label class="mb-1.5 block text-xs font-medium text-gray-600 dark:text-gray-400">Model</label>
					<input
						v-model="agentModelInput"
						type="text"
						class="w-full rounded-lg border border-gray-300 bg-white px-3 py-2.5 text-sm text-gray-900 placeholder-gray-400 focus:border-blue-500 focus:outline-none dark:border-gray-700 dark:bg-gray-800 dark:text-white dark:placeholder-gray-500"
					/>
					<p class="mt-1 text-xs text-gray-400 dark:text-gray-500">Format: <code class="font-mono">provider/model-name</code></p>
				</div>
				<div v-if="agentError" class="rounded-lg bg-red-50 px-3 py-2 text-xs text-red-700 dark:bg-red-950 dark:text-red-400">
					{{ agentError }}
				</div>
			</div>

			<div class="mt-6 flex justify-end">
				<button
					type="button"
					:disabled="!agentName.trim() || !agentModelInput.trim() || agentSaving"
					class="rounded-lg bg-blue-600 px-5 py-2.5 text-sm font-semibold text-white hover:bg-blue-500 disabled:opacity-40"
					@click="createAgent"
				>
					{{ agentSaving ? 'Creating…' : 'Create agent →' }}
				</button>
			</div>
		</div>

		<!-- Step: done -->
		<div v-else-if="step === 'done'" class="text-center">
			<div class="mb-6 flex justify-center">
				<span class="flex h-16 w-16 items-center justify-center rounded-full bg-green-100 dark:bg-green-900/40">
					<svg class="h-8 w-8 text-green-600 dark:text-green-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
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
				<router-link
					to="/settings?tab=agents"
					class="rounded-lg border border-gray-200 px-4 py-2.5 text-sm font-medium text-gray-700 hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800"
				>
					Explore settings
				</router-link>
				<router-link
					to="/chat"
					class="rounded-lg bg-blue-600 px-5 py-2.5 text-sm font-semibold text-white hover:bg-blue-500"
				>
					Start chatting →
				</router-link>
			</div>
		</div>
	</div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useMCP } from "../composables/useMCP";
import { type AppConfig, useSettingsStore } from "../stores/settings";

defineEmits<{ skip: [] }>();

interface Provider {
	id: string;
	name: string;
	emoji: string;
	description: string;
	credentialHint: string;
	keyPlaceholder: string;
	keyHelp?: string;
	authKey: string;
	defaultModel: string;
}

const providers: Provider[] = [
	{
		id: "anthropic",
		name: "Anthropic",
		emoji: "🤖",
		description: "Claude — great for coding & reasoning",
		credentialHint: "Enter your Anthropic API key to get started.",
		keyPlaceholder: "sk-ant-...",
		keyHelp: "Find your key at console.anthropic.com → API Keys.",
		authKey: "anthropic:default",
		defaultModel: "anthropic/claude-sonnet-4-5",
	},
	{
		id: "openai",
		name: "OpenAI",
		emoji: "🧠",
		description: "GPT-4o — versatile and capable",
		credentialHint: "Enter your OpenAI API key to get started.",
		keyPlaceholder: "sk-...",
		keyHelp: "Find your key at platform.openai.com → API keys.",
		authKey: "openai:default",
		defaultModel: "openai/gpt-4o",
	},
	{
		id: "gemini",
		name: "Gemini",
		emoji: "✨",
		description: "Google Gemini — fast and multimodal",
		credentialHint: "Enter your Google AI Studio API key to get started.",
		keyPlaceholder: "AIza...",
		keyHelp: "Find your key at aistudio.google.com → Get API key.",
		authKey: "gemini:default",
		defaultModel: "gemini/gemini-2.0-flash",
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

const apiKey = ref("");
const credSaving = ref(false);
const credError = ref("");

const agentName = ref("assistant");
const agentModelInput = ref("");

// Pre-fill model when provider is chosen
watch(currentProvider, (p) => {
	if (p && !agentModelInput.value) {
		agentModelInput.value = p.defaultModel;
	}
});

const agentSaving = ref(false);
const agentError = ref("");

async function saveCredential() {
	if (!apiKey.value.trim() || !currentProvider.value) return;
	credSaving.value = true;
	credError.value = "";
	try {
		await callTool("auth_set", {
			name: currentProvider.value.authKey,
			value: apiKey.value.trim(),
		});
		// Pre-fill model now that provider is confirmed
		if (!agentModelInput.value) {
			agentModelInput.value = currentProvider.value.defaultModel;
		}
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
			port: 16677,
			tls: { cert: "", key: "" },
			external_access: false,
			no_tls: false,
		},
		agents: [],
		models: { providers: {}, defaults: { model: "", fallbacks: [] } },
		browser: { binary: "", cdp_port: 9222 },
		scheduler: { concurrency: "auto" },
	};
}
</script>
