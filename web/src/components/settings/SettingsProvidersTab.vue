<template>
				<section v-show="activeTab === 'providers'" class="space-y-5 pb-8">
					<!-- Provider Authentication -->
					<div class="rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
						<h3 class="mb-2 text-sm font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Credentials
						</h3>
						<p class="mb-4 text-xs text-gray-500 dark:text-gray-400">Configure authentication for LLM providers. OAuth
							tokens are stored securely and refreshed automatically.</p>

						<!-- Existing provider credentials -->
						<div v-if="configuredProviders.length"
							class="mb-4 overflow-hidden rounded-lg border border-gray-200 dark:border-gray-700">
							<table class="w-full text-xs">
								<thead>
									<tr class="border-b border-gray-200 bg-gray-50 dark:border-gray-700 dark:bg-gray-800/50">
										<th class="px-3 py-2 text-left font-medium text-gray-500 dark:text-gray-400">Provider</th>
										<th class="px-3 py-2 text-left font-medium text-gray-500 dark:text-gray-400">Auth Type</th>
										<th class="px-3 py-2 text-left font-medium text-gray-500 dark:text-gray-400">Status</th>
										<th class="w-36 px-3 py-2"></th>
									</tr>
								</thead>
								<tbody>
									<tr v-for="entry in configuredProviders" :key="entry.key"
										class="border-b border-gray-100 last:border-0 dark:border-gray-800">
										<td class="px-3 py-2 font-medium text-gray-800 dark:text-gray-200">{{ entry.providerLabel }}</td>
										<td class="px-3 py-2">
											<span
												:class="entry.authType === 'oauth'
													? 'inline-block rounded bg-blue-100 px-1.5 py-0.5 text-xs font-medium text-blue-700 dark:bg-blue-900/30 dark:text-blue-300'
													: entry.authType === 'endpoint'
														? 'inline-block rounded bg-emerald-100 px-1.5 py-0.5 text-xs font-medium text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
														: 'inline-block rounded bg-gray-100 px-1.5 py-0.5 text-xs font-medium text-gray-600 dark:bg-gray-800 dark:text-gray-300'">
												{{ entry.authType === 'oauth' ? 'OAuth' : entry.authType === 'endpoint' ? 'Endpoint' : 'API Key' }}
											</span>
										</td>
										<td class="px-3 py-2">
											<span v-if="entry.authType === 'oauth'"
												class="inline-flex items-center gap-1 text-green-600 dark:text-green-400">
												<svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5" viewBox="0 0 20 20"
													fill="currentColor">
													<path fill-rule="evenodd"
														d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z"
														clip-rule="evenodd" />
												</svg>
												Authorized
											</span>
											<div v-else-if="entry.authType === 'endpoint'" class="space-y-1">
												<div class="font-mono text-[11px] text-gray-500 dark:text-gray-400">{{ entry.baseURI }}</div>
												<div class="text-[11px] text-gray-400 dark:text-gray-500">
													{{ entry.hasAPIKey ? "Bearer token configured" : "No bearer token" }}
												</div>
											</div>
											<span v-else class="tracking-widest text-gray-400 dark:text-gray-500">••••••••</span>
										</td>
										<td class="px-3 py-2 text-right">
											<div class="flex items-center justify-end gap-2">
												<button v-if="entry.authType === 'oauth'" type="button"
													class="text-xs text-blue-600 hover:underline disabled:opacity-50 dark:text-blue-400"
													:disabled="oauthBusy" @click="reauthorizeProvider(entry.provider)">Re-authorize</button>
												<button v-if="entry.authType === 'endpoint'" type="button"
													class="text-xs text-blue-600 hover:underline dark:text-blue-400"
													@click="providerAddSelection = `${entry.provider}:endpoint`">Edit</button>
												<button type="button"
													class="text-gray-400 hover:text-red-500 dark:text-gray-500 dark:hover:text-red-400"
													:title="`Remove ${entry.key}`"
													@click="entry.authType === 'endpoint' ? deleteProviderConnection(entry.provider) : deleteProviderCredential(entry.key)">
													<svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5" viewBox="0 0 20 20"
														fill="currentColor">
														<path fill-rule="evenodd"
															d="M9 2a1 1 0 00-.894.553L7.382 4H4a1 1 0 000 2v10a2 2 0 002 2h8a2 2 0 002-2V6a1 1 0 100-2h-3.382l-.724-1.447A1 1 0 0011 2H9zM7 8a1 1 0 012 0v6a1 1 0 11-2 0V8zm5-1a1 1 0 00-1 1v6a1 1 0 102 0V8a1 1 0 00-1-1z"
															clip-rule="evenodd" />
													</svg>
												</button>
											</div>
										</td>
									</tr>
								</tbody>
							</table>
						</div>

						<!-- Add provider credential -->
						<div v-if="availableProviderOptions.length" class="flex flex-wrap items-center gap-2">
							<select v-model="providerAddSelection" class="field-input max-w-[220px]">
								<option value="">Add provider…</option>
								<option v-for="opt in availableProviderOptions" :key="opt.key" :value="opt.key">{{ opt.label }}</option>
							</select>
							<template v-if="providerAddSelection">
								<template v-if="providerAddSelection.endsWith(':endpoint')">
									<input v-model="providerBaseURIValue" type="text" class="field-input min-w-[260px] max-w-[320px]"
										placeholder="Base URI (optional)" />
									<input v-model="providerApiKeyValue" type="password" class="field-input max-w-[240px]"
										placeholder="Bearer token (optional)" />
									<button type="button"
										class="rounded-lg bg-blue-600 px-3 py-2 text-xs font-semibold text-white hover:bg-blue-500 disabled:opacity-50"
										@click="addProviderEndpoint">
										Save
									</button>
								</template>
								<template v-if="providerAddSelection.endsWith(':apikey')">
									<input v-model="providerApiKeyValue" type="password" class="field-input max-w-[260px]"
										placeholder="API key…" />
									<button type="button"
										class="rounded-lg bg-blue-600 px-3 py-2 text-xs font-semibold text-white hover:bg-blue-500 disabled:opacity-50"
										:disabled="!providerApiKeyValue.trim()" @click="addProviderApiKey">Add</button>
								</template>
								<button v-else-if="providerAddSelection.endsWith(':oauth')" type="button"
									class="rounded-lg bg-blue-600 px-3 py-2 text-xs font-semibold text-white hover:bg-blue-500 disabled:opacity-50"
									:disabled="oauthBusy" @click="addProviderOAuth">
									{{ oauthBusy ? 'Authorizing…' : 'Authorize' }}
								</button>
							</template>
						</div>
						<p v-else-if="!configuredProviders.length" class="text-xs text-gray-400 dark:text-gray-500">No providers
							configured yet.
							Use the dropdown above to add one.</p>

						<!-- Anthropic two-step OAuth inline form -->
						<div v-if="anthropicUrl" class="mt-3 space-y-2 rounded-lg border border-gray-200 p-3 dark:border-gray-700">
							<p class="text-xs text-gray-500 dark:text-gray-400">Open the link below, sign in, and paste the code
								shown:</p>
							<a :href="anthropicUrl" target="_blank" rel="noreferrer"
								class="block truncate text-xs text-blue-600 hover:text-blue-500 dark:text-blue-400">{{ anthropicUrl }}</a>
							<div class="flex gap-2">
								<input v-model="anthropicCode" type="text" class="field-input" placeholder="Paste code here…" />
								<button type="button"
									class="rounded-lg bg-blue-600 px-3 py-2 text-xs font-semibold text-white hover:bg-blue-500 disabled:opacity-50"
									:disabled="oauthBusy || !anthropicCode.trim()" @click="completeAnthropic">Complete</button>
							</div>
						</div>

						<div v-if="openAIUrl" class="mt-3 space-y-2 rounded-lg border border-gray-200 p-3 dark:border-gray-700">
							<p class="text-xs text-gray-500 dark:text-gray-400">Open the link below and complete OpenAI Codex sign-in on a machine that can receive the callback:</p>
							<a :href="openAIUrl" target="_blank" rel="noreferrer"
								class="block break-all text-xs text-blue-600 hover:text-blue-500 dark:text-blue-400">{{ openAIUrl }}</a>
							<p class="text-xs text-gray-500 dark:text-gray-400">Callback URL: <span class="font-mono">{{ openAICallbackUrl }}</span></p>
							<p
								:class="openAITimedOut ? 'text-xs font-medium text-red-600 dark:text-red-400' : 'text-xs text-gray-500 dark:text-gray-400'">
								{{ openAITimedOut ? 'This OpenAI Codex callback timed out. Start the flow again.' : `Callback expires in ${formatCountdown(openAIRemainingSeconds)}.` }}
							</p>
							<button type="button"
								class="w-full rounded-lg bg-blue-600 px-3 py-2 text-xs font-semibold text-white hover:bg-blue-500 disabled:opacity-50"
								:disabled="oauthBusy || openAITimedOut" @click="completeOpenAI">
								{{ openAITimedOut ? 'Timed out' : oauthBusy ? 'Waiting for authorization…' : "I've authorized — Complete" }}
							</button>
						</div>

						<div v-if="geminiUrl" class="mt-3 space-y-2 rounded-lg border border-gray-200 p-3 dark:border-gray-700">
							<p class="text-xs text-gray-500 dark:text-gray-400">Open the link below and complete Gemini sign-in on a machine that can receive the callback:</p>
							<a :href="geminiUrl" target="_blank" rel="noreferrer"
								class="block break-all text-xs text-blue-600 hover:text-blue-500 dark:text-blue-400">{{ geminiUrl }}</a>
							<p class="text-xs text-gray-500 dark:text-gray-400">Callback URL: <span class="font-mono">{{ geminiCallbackUrl }}</span></p>
							<p
								:class="geminiTimedOut ? 'text-xs font-medium text-red-600 dark:text-red-400' : 'text-xs text-gray-500 dark:text-gray-400'">
								{{ geminiTimedOut ? 'This Gemini callback timed out. Start the flow again.' : `Callback expires in ${formatCountdown(geminiRemainingSeconds)}.` }}
							</p>
							<button type="button"
								class="w-full rounded-lg bg-blue-600 px-3 py-2 text-xs font-semibold text-white hover:bg-blue-500 disabled:opacity-50"
								:disabled="oauthBusy || geminiTimedOut" @click="completeGemini">
								{{ geminiTimedOut ? 'Timed out' : oauthBusy ? 'Waiting for authorization…' : "I've authorized — Complete" }}
							</button>
						</div>

						<!-- GitHub Copilot device-flow inline form -->
						<div
							v-if="copilotUserCode"
							id="copilot-device-flow"
							class="mt-3 space-y-3 rounded-lg border border-gray-200 p-3 dark:border-gray-700"
						>
							<div class="rounded-md bg-blue-50 px-3 py-2 text-xs text-blue-800 dark:bg-blue-950/40 dark:text-blue-200">
								Enter the code on GitHub's page, not in Aviary. After GitHub confirms authorization, come back here and click Complete.
							</div>
							<div class="rounded-md border border-gray-200 bg-gray-50 p-3 dark:border-gray-700 dark:bg-gray-800">
								<p class="text-[11px] font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Open GitHub device page</p>
								<a :href="copilotVerifyUrl" target="_blank" rel="noreferrer"
									class="mt-1 block break-all text-xs text-blue-600 hover:underline dark:text-blue-400">{{ copilotVerifyUrl }}</a>
							</div>
							<div class="rounded-md border border-gray-200 bg-gray-50 p-3 dark:border-gray-700 dark:bg-gray-800">
								<p class="text-[11px] font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">One-time code to enter on GitHub</p>
								<div class="mt-2 flex items-center gap-2">
									<input
										:value="copilotUserCode"
										readonly
										type="text"
										class="field-input flex-1 bg-white py-2 text-center font-mono text-lg font-bold tracking-widest text-gray-900 dark:bg-gray-900 dark:text-white"
										@click="selectCode"
									/>
									<button
										type="button"
										class="rounded-md border border-gray-200 px-3 py-2 text-xs font-semibold text-gray-700 hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-700"
										@click="copyCode"
									>
										{{ copyLabel }}
									</button>
								</div>
							</div>
							<button type="button"
								class="w-full rounded-lg bg-gray-900 px-3 py-2 text-xs font-semibold text-white hover:bg-gray-700 disabled:opacity-50 dark:bg-gray-700 dark:hover:bg-gray-600"
								:disabled="oauthBusy" @click="completeCopilot">
								{{ oauthBusy ? 'Waiting for authorization…' : "I\'ve authorized on GitHub — Complete" }}
							</button>
						</div>
					</div>

					<!-- Extra Secrets -->
					<div class="rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
						<h3 class="mb-2 text-sm font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Extra
							Secrets</h3>
						<p class="mb-4 text-xs text-gray-500 dark:text-gray-400">Store arbitrary secrets for use by tools and agents
							(e.g. a
							Brave API key or Twilio auth token).</p>
						<div class="overflow-hidden rounded-lg border border-gray-200 dark:border-gray-700">
							<table class="w-full text-xs">
								<thead>
									<tr class="border-b border-gray-200 bg-gray-50 dark:border-gray-700 dark:bg-gray-800/50">
										<th class="px-3 py-2 text-left font-medium text-gray-500 dark:text-gray-400">Name</th>
										<th class="px-3 py-2 text-left font-medium text-gray-500 dark:text-gray-400">Value</th>
										<th class="w-8 px-3 py-2"></th>
									</tr>
								</thead>
								<tbody>
									<tr class="border-b border-gray-200 dark:border-gray-700">
										<td class="px-2 py-1.5">
											<input v-model="secretName" type="text" class="field-input py-1.5 font-mono text-xs"
												placeholder="brave_api_key" />
										</td>
										<td class="px-2 py-1.5">
											<input v-model="secretValue" type="password" class="field-input py-1.5 text-xs" placeholder="…" />
										</td>
										<td class="px-2 py-1.5">
											<button type="button"
												class="rounded-lg bg-blue-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-blue-500"
												@click="addSecret">Add</button>
										</td>
									</tr>
									<tr v-for="name in extraSecrets" :key="name"
										class="border-b border-gray-100 last:border-0 dark:border-gray-800">
										<td class="px-3 py-2 font-mono text-gray-700 dark:text-gray-300">{{ name }}</td>
										<td class="px-3 py-2 tracking-widest text-gray-400 dark:text-gray-500">••••••••</td>
										<td class="px-3 py-2 text-right">
											<button type="button"
												class="text-gray-400 hover:text-red-500 dark:text-gray-500 dark:hover:text-red-400"
												:title="`Delete ${name}`" @click="deleteSecret(name)">
												<svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5" viewBox="0 0 20 20"
													fill="currentColor">
													<path fill-rule="evenodd"
														d="M9 2a1 1 0 00-.894.553L7.382 4H4a1 1 0 000 2v10a2 2 0 002 2h8a2 2 0 002-2V6a1 1 0 100-2h-3.382l-.724-1.447A1 1 0 0011 2H9zM7 8a1 1 0 012 0v6a1 1 0 11-2 0V8zm5-1a1 1 0 00-1 1v6a1 1 0 102 0V8a1 1 0 00-1-1z"
														clip-rule="evenodd" />
												</svg>
											</button>
										</td>
									</tr>
									<tr v-if="!extraSecrets.length">
										<td colspan="3" class="px-3 py-3 text-center text-gray-400 dark:text-gray-500">No extra secrets
											stored yet.
										</td>
									</tr>
								</tbody>
							</table>
						</div>
						<button type="button"
							class="mt-2 text-xs text-gray-400 hover:text-gray-600 dark:text-gray-500 dark:hover:text-gray-300"
							@click="refreshCredentials">? Refresh</button>
					</div>

					<div class="rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
						<h3 class="mb-2 text-sm font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Token</h3>
						<p class="text-xs text-gray-500 dark:text-gray-400">
							The Aviary token authenticates access to the web UI and API. To rotate it, run the command below in your
							terminal.
							Regenerating the token signs out existing sessions and clients using the old token.
						</p>
						<div
							class="mt-4 rounded-lg border border-gray-200 bg-gray-50 px-3 py-2 font-mono text-xs text-gray-700 dark:border-gray-700 dark:bg-gray-800/60 dark:text-gray-200">
							aviary token --new
						</div>
					</div>
				</section>
</template>

<script lang="ts">
import { computed, defineComponent, inject, ref, toRefs } from "vue";
import { settingsViewContextKey } from "./context";

interface ConfiguredProviderEntry {
	key: string;
	provider: string;
	providerLabel: string;
	authType: "oauth" | "endpoint" | "apikey";
	baseURI?: string;
	hasAPIKey?: boolean;
}

interface AvailableProviderOption {
	key: string;
	label: string;
}

interface SettingsProvidersContext {
	activeTab: string;
	configuredProviders: ConfiguredProviderEntry[];
	oauthBusy: boolean;
	reauthorizeProvider: (provider: string) => unknown;
	providerAddSelection: string;
	deleteProviderConnection: (provider: string) => unknown;
	deleteProviderCredential: (key: string) => unknown;
	availableProviderOptions: AvailableProviderOption[];
	providerBaseURIValue: string;
	providerApiKeyValue: string;
	addProviderEndpoint: () => unknown;
	addProviderApiKey: () => unknown;
	addProviderOAuth: () => unknown;
	anthropicUrl: string;
	anthropicCode: string;
	completeAnthropic: () => unknown;
	openAIUrl: string;
	openAICallbackUrl: string;
	openAITimedOut: boolean;
	openAIRemainingSeconds: number | null;
	formatCountdown: (seconds: number | null) => string;
	completeOpenAI: () => unknown;
	geminiUrl: string;
	geminiCallbackUrl: string;
	geminiTimedOut: boolean;
	geminiRemainingSeconds: number | null;
	completeGemini: () => unknown;
	copilotUserCode: string;
	copilotVerifyUrl: string;
	completeCopilot: () => unknown;
	secretName: string;
	secretValue: string;
	addSecret: () => unknown;
	extraSecrets: string[];
	deleteSecret: (name: string) => unknown;
	refreshCredentials: () => unknown;
}

export default defineComponent({
	name: "SettingsProvidersTab",
	setup() {
		const settings = inject<SettingsProvidersContext>(settingsViewContextKey);
		if (!settings) {
			throw new Error("Settings view context is not available.");
		}
		const resolvedSettings = settings;
		const context = toRefs(resolvedSettings);
		const copiedCode = ref(false);

		async function copyText(text: string) {
			if (!text) return;
			try {
				await navigator.clipboard.writeText(text);
				return;
			} catch {
				const input = document.createElement("input");
				input.value = text;
				input.setAttribute("readonly", "true");
				input.style.position = "absolute";
				input.style.left = "-9999px";
				document.body.appendChild(input);
				input.select();
				document.execCommand("copy");
				document.body.removeChild(input);
			}
		}

		async function copyCode() {
			await copyText(resolvedSettings.copilotUserCode);
			copiedCode.value = true;
			window.setTimeout(() => {
				copiedCode.value = false;
			}, 1500);
		}

		function selectCode(event: Event) {
			(event.target as HTMLInputElement | null)?.select();
		}

		const copyLabel = computed(() => (copiedCode.value ? "Copied" : "Copy"));

		const exposed = {
			...context,
			copyCode,
			copyLabel,
			selectCode,
		};
		return exposed;
	},
});
</script>

