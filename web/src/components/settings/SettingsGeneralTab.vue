<template>
				<section v-show="activeTab === 'general'" class="space-y-6 pb-8">
					<div class="rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
						<h3 class="mb-4 text-sm font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Server</h3>
						<div class="grid gap-4 lg:grid-cols-3">
							<div>
								<label class="field-label">Port</label>
								<input :value="serverPortInput" type="text" inputmode="numeric" pattern="[0-9]*" class="field-input"
									placeholder="16677" @input="updateServerPortInput" />
							</div>
							<div>
								<label class="field-label">TLS Cert</label>
								<input v-model="draft.server.tls.cert" type="text" class="field-input"
									placeholder="/path/to/cert.pem" />
							</div>
							<div>
								<label class="field-label">TLS Key</label>
								<input v-model="draft.server.tls.key" type="text" class="field-input" placeholder="/path/to/key.pem" />
							</div>
						</div>
						<div class="mt-4 flex flex-wrap gap-6">
							<label class="flex cursor-pointer items-center gap-3">
								<input v-model="draft.server.external_access" type="checkbox"
									class="h-4 w-4 rounded border-gray-300 text-blue-600 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800" />
								<span class="text-sm text-gray-700 dark:text-gray-300">
									Expose service externally
									<span class="ml-1 text-xs text-gray-400 dark:text-gray-500">(bind to 0.0.0.0 instead of
										127.0.0.1)</span>
								</span>
							</label>
							<label class="flex cursor-pointer items-center gap-3">
								<input v-model="draft.server.no_tls" type="checkbox"
									class="h-4 w-4 rounded border-gray-300 text-blue-600 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800" />
								<span class="text-sm text-gray-700 dark:text-gray-300">
									Disable TLS
									<span class="ml-1 text-xs text-gray-400 dark:text-gray-500">(plain HTTP — not recommended)</span>
								</span>
							</label>
						</div>
						<p v-if="draft.server.external_access || draft.server.no_tls"
							class="mt-3 rounded-lg bg-amber-50 px-3 py-2 text-xs text-amber-700 dark:bg-amber-950 dark:text-amber-300">
							Changing server settings will restart the service.
						</p>
					</div>

					<div class="rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
						<h3 class="mb-4 text-sm font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Models</h3>
						<div class="grid gap-4 lg:grid-cols-2">
							<div>
								<label class="field-label">Default model</label>
								<ModelSelector v-model="draft.models.defaults.model" :options="availableModelOptions"
									placeholder="Select a model…" />
							</div>
							<div>
								<label class="field-label">Default fallbacks</label>
								<ModelSelector v-model="draft.models.defaults.fallbacks" :options="availableModelOptions" multiple
									placeholder="Add fallbacks…" />
							</div>
						</div>
					</div>

					<div class="rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
						<h3 class="mb-4 text-sm font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Browser</h3>
						<div class="grid gap-4 lg:grid-cols-2">
							<div>
								<label class="field-label">Browser binary</label>
								<input v-model="draft.browser.binary" type="text" class="field-input" placeholder="/usr/bin/chromium" />
							</div>
							<div>
								<label class="field-label">CDP port</label>
								<input :value="cdpPortInput" type="text" inputmode="numeric" pattern="[0-9]*" class="field-input"
									placeholder="9222" @input="updateCDPPortInput" />
							</div>
						</div>
						<div class="mt-4 flex flex-wrap gap-6">
							<label class="flex cursor-pointer items-center gap-3">
								<input v-model="draft.browser.headless" type="checkbox"
									class="h-4 w-4 rounded border-gray-300 text-blue-600 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800" />
								<span class="text-sm text-gray-700 dark:text-gray-300">
									Run headless
									<span class="block text-xs text-gray-500 dark:text-gray-400">No visible browser window</span>
								</span>
							</label>
							<label class="flex cursor-pointer items-center gap-3">
								<input v-model="draft.browser.reuse_tabs" type="checkbox"
									class="h-4 w-4 rounded border-gray-300 text-blue-600 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800" />
								<span class="text-sm text-gray-700 dark:text-gray-300">
									Reuse matching tabs
									<span class="block text-xs text-gray-500 dark:text-gray-400">browser_open reuses an existing tab when the URL matches exactly</span>
								</span>
							</label>
						</div>
					</div>

					<div class="rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
						<h3 class="mb-4 text-sm font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Scheduler</h3>
						<div class="grid gap-4 lg:grid-cols-2">
							<div>
								<label class="field-label">Concurrency</label>
								<input v-model="concurrencyInput" type="text" class="field-input" placeholder="auto or number" />
							</div>
						</div>
					</div>

					<div class="rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
						<h3 class="mb-2 text-sm font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Web Search
						</h3>
						<p class="mb-4 text-xs text-gray-500 dark:text-gray-400">Select the stored secret that holds your Brave
							Search API key. This writes an <span class="font-mono">auth:&lt;name&gt;</span> reference into <span
								class="font-mono">aviary.yaml</span>.</p>
						<div class="flex flex-wrap items-center gap-2">
							<select v-model="webSearchSecretSelection" class="field-input max-w-[320px]">
								<option value="">Use browser fallback only</option>
								<option v-for="name in webSearchSecretOptions" :key="name" :value="name">{{ name }}</option>
								<option :value="WEB_SEARCH_ADD_SECRET_OPTION">Add new secret</option>
							</select>
							<span v-if="draft.search.web.brave_api_key"
								class="rounded bg-gray-100 px-2 py-1 font-mono text-xs text-gray-600 dark:bg-gray-800 dark:text-gray-300">
								{{ draft.search.web.brave_api_key }}
							</span>
						</div>
						<p v-if="!webSearchSecretOptions.length" class="mt-3 text-xs text-gray-400 dark:text-gray-500">No stored
							secrets available yet. Choose <span class="font-medium text-gray-500 dark:text-gray-400">Add new
								secret</span> to create one here.</p>
					</div>
				</section>
</template>

<script lang="ts">
import { defineComponent, inject } from "vue";
import ModelSelector from "../ModelSelector.vue";
import { settingsViewContextKey } from "./context";

export default defineComponent({
	name: "SettingsGeneralTab",
	components: { ModelSelector },
	setup() {
		const settings = inject(settingsViewContextKey);
		if (!settings) {
			throw new Error("Settings view context is not available.");
		}
		return settings;
	},
});
</script>

