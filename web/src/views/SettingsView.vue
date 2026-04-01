<template>
	<AppLayout>
		<div class="h-full overflow-y-auto">
			<div class="mx-auto max-w-7xl px-4 py-6 sm:px-6">
				<div
					class="sticky top-0 z-20 -mx-4 mb-6 border-b border-gray-200/80 bg-white/90 px-4 py-4 backdrop-blur sm:-mx-6 sm:px-6 dark:border-gray-800/80 dark:bg-gray-950/88">
					<div class="flex items-center justify-between gap-3">
						<h2 class="text-xl font-bold text-gray-900 dark:text-white">Settings</h2>
						<div class="flex items-center gap-2">
							<transition name="save-indicator">
								<div v-if="saveSuccessVisible" class="flex items-center gap-1.5 text-emerald-600 dark:text-emerald-400"
									:aria-label="headerNoticeText" :title="headerNoticeText">
									<svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" viewBox="0 0 20 20" fill="currentColor"
										aria-hidden="true">
										<path fill-rule="evenodd"
											d="M10 18a8 8 0 1 0 0-16 8 8 0 0 0 0 16Zm3.78-9.72a.75.75 0 0 0-1.06-1.06L9.25 10.69 7.78 9.22a.75.75 0 1 0-1.06 1.06l2 2a.75.75 0 0 0 1.06 0l4-4Z"
											clip-rule="evenodd" />
									</svg>
									<span class="text-xs font-medium">{{ headerNoticeText }}</span>
								</div>
							</transition>
							<button v-if="revertAvailable" type="button"
								class="rounded-lg border border-amber-200 px-3 py-2 text-xs text-amber-700 hover:bg-amber-50 disabled:opacity-50 dark:border-amber-900 dark:text-amber-300 dark:hover:bg-amber-950"
								:disabled="loading || saving || reverting"
								@click="revertToLatestBackup">{{ reverting ? "Reverting…" : "Revert" }}</button>
							<button type="button"
								class="rounded-lg border border-gray-200 px-3 py-2 text-xs text-gray-700 hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800"
								:disabled="loading || saving || reverting"
								@click="loadConfig">{{ loading ? "Loading…" : "Reload" }}</button>
							<button type="button"
								class="rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white hover:bg-blue-500 disabled:opacity-50"
								:disabled="saving || reverting" @click="saveAll()">{{ saving ? "Saving…" : "Save Changes" }}</button>
						</div>
					</div>
				</div>

				<div v-if="errorMessage"
					class="mb-4 rounded-lg bg-red-50 px-4 py-3 text-sm text-red-700 dark:bg-red-950 dark:text-red-300">
					{{ errorMessage }}
				</div>
				<div v-if="okMessage"
					class="mb-4 rounded-lg bg-green-50 px-4 py-3 text-sm text-green-700 dark:bg-green-950 dark:text-green-300">
					{{ okMessage }}
				</div>
				<div v-if="compileToastVisible"
					class="mb-4 flex items-center justify-between rounded-lg bg-blue-50 px-4 py-3 text-sm text-blue-700 dark:bg-blue-950 dark:text-blue-300">
					<span>Compile in progress… <RouterLink to="/jobs" class="font-medium underline">View jobs</RouterLink></span>
					<button type="button" class="ml-4 text-blue-500 hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-200" @click="compileToastVisible = false">✕</button>
				</div>

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
							</select>
							<span v-if="draft.search.web.brave_api_key"
								class="rounded bg-gray-100 px-2 py-1 font-mono text-xs text-gray-600 dark:bg-gray-800 dark:text-gray-300">
								{{ draft.search.web.brave_api_key }}
							</span>
						</div>
						<p v-if="!webSearchSecretOptions.length" class="mt-3 text-xs text-gray-400 dark:text-gray-500">No stored
							secrets available yet. Add one in Providers & Auth, then select it here.</p>
					</div>
				</section>

				<section v-show="activeTab === 'agents'" class="space-y-5 pb-8">
					<div class="flex items-center border-b border-gray-200 dark:border-gray-800">
						<div class="scrollbar-none flex flex-1 items-end overflow-x-auto">
							<button v-for="(a, idx) in draft.agents" :key="`tab-${idx}`" type="button"
								class="-mb-px shrink-0 border-b-2 px-4 py-2.5 text-sm transition-colors"
								:class="selectedAgentIdx === idx
									? 'border-blue-600 font-semibold text-blue-700 dark:border-blue-400 dark:text-blue-400'
									: 'border-transparent font-medium text-gray-500 hover:border-gray-300 hover:text-gray-800 dark:text-gray-400 dark:hover:border-gray-600 dark:hover:text-gray-200'"
								@click="selectedAgentIdx = idx">
								{{ a.name || `Agent ${idx + 1}` }}
							</button>
							<button type="button" aria-label="Add Agent" title="Add agent"
								class="-mb-px shrink-0 border-b-2 border-transparent px-3 py-2.5 text-lg leading-none text-gray-400 transition-colors hover:text-blue-600 dark:text-gray-500 dark:hover:text-blue-400"
								@click="addAgent">+</button>
						</div>
					</div>

					<div v-if="!draft.agents.length"
						class="rounded-xl border border-gray-200 bg-white px-4 py-3 text-sm text-gray-500 dark:border-gray-800 dark:bg-gray-900 dark:text-gray-400">
						No agents configured.
					</div>

					<div v-for="{ agent, i } in selectedAgentAsSingletonList" :key="`agent-${i}`"
						class="rounded-xl border border-gray-200 bg-white dark:border-gray-800 dark:bg-gray-900">
						<!-- Subtab nav -->
						<div class="border-b border-gray-200 px-5 dark:border-gray-700">
							<nav class="flex items-center justify-between gap-4">
								<div class="flex items-center">
									<button v-for="subtab in ([
										{ key: 'general', label: 'General' },
										{ key: 'permissions', label: 'Permissions' },
										{ key: 'channels', label: 'Channels' },
										{ key: 'tasks', label: 'Tasks' },
									] as const)" :key="subtab.key" type="button" class="-mb-px border-b-2 px-4 py-2.5 text-sm transition-colors"
										:class="selectedAgentSubtab === subtab.key
											? 'border-blue-600 font-semibold text-blue-700 dark:border-blue-400 dark:text-blue-400'
											: 'border-transparent font-medium text-gray-500 hover:border-gray-300 hover:text-gray-800 dark:text-gray-400 dark:hover:border-gray-600 dark:hover:text-gray-200'"
										@click="selectedAgentSubtab = subtab.key">
										{{ subtab.label }}
									</button>
								</div>
								<div class="flex items-center">
									<button type="button"
										class="-mb-px border-b-2 border-transparent px-4 py-2.5 text-sm font-medium text-red-600 transition-colors hover:border-red-200 hover:text-red-700 dark:text-red-400 dark:hover:border-red-900 dark:hover:text-red-300"
										@click="removeAgent(i)">
										Remove Agent
									</button>
								</div>
							</nav>
						</div>

						<!-- General subtab -->
						<div v-show="selectedAgentSubtab === 'general'" class="space-y-4 p-5">
							<div class="grid gap-4 lg:grid-cols-[1fr_1fr_1.5fr]">
								<div>
									<label class="field-label">Name</label>
									<input v-model="agent.name" type="text" class="field-input" placeholder="assistant"
										@change="onAgentNameChange(agent)" />
								</div>
								<div>
									<label class="field-label">Model</label>
									<ModelSelector v-model="agent.model" :options="availableModelOptions" placeholder="Select a model…" />
								</div>
								<div>
									<label class="field-label">Fallbacks</label>
									<ModelSelector v-model="agent.fallbacks" :options="availableModelOptions" multiple
										placeholder="Add fallbacks…" />
								</div>
							</div>
							<div>
								<label class="field-label">Working directory</label>
								<input v-model="agent.working_dir" type="text" class="field-input"
									placeholder="Default: process working directory (e.g. /home/user/projects/myrepo)" />
							</div>
							<div class="mt-2">
								<label class="flex cursor-pointer items-center gap-3">
									<input v-model="agent.verbose" type="checkbox"
										class="h-4 w-4 rounded border-gray-300 text-blue-600 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800" />
									<span class="text-sm text-gray-700 dark:text-gray-300">
										Verbose mode
										<span class="ml-1 text-xs text-gray-400 dark:text-gray-500">(send live status updates before each tool call on channels that don't support streaming)</span>
									</span>
								</label>
							</div>
						</div>

						<!-- Files content moved into General subtab -->
						<div v-show="selectedAgentSubtab === 'general'" class="px-5 pb-5">
							<div class="space-y-2 rounded-xl border border-gray-200 p-3 dark:border-gray-700">
								<div class="flex items-center justify-between gap-2">
									<div class="text-[11px] font-medium uppercase tracking-wide text-gray-400 dark:text-gray-500">Files
									</div>
									<div class="flex flex-wrap items-center gap-1.5">
										<transition name="save-indicator">
											<div v-if="getAgentFileState(agent.name).saveFlash"
												class="flex items-center gap-1 text-emerald-600 dark:text-emerald-400">
												<svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" viewBox="0 0 20 20" fill="currentColor"
													aria-hidden="true">
													<path fill-rule="evenodd"
														d="M10 18a8 8 0 1 0 0-16 8 8 0 0 0 0 16Zm3.78-9.72a.75.75 0 0 0-1.06-1.06L9.25 10.69 7.78 9.22a.75.75 0 1 0-1.06 1.06l2 2a.75.75 0 0 0 1.06 0l4-4Z"
														clip-rule="evenodd" />
												</svg>
												<span class="text-[11px] font-medium">Saved</span>
											</div>
										</transition>
										<button type="button"
											class="rounded-md border border-gray-200 px-2 py-1 text-[11px] text-gray-700 hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800 disabled:opacity-40"
											:disabled="!agent.name || getAgentFileState(agent.name).loading"
											@click="loadAgentFiles(agent.name)">
											{{ getAgentFileState(agent.name).loading ? 'Loading…' : 'Refresh' }}
										</button>
										<button type="button"
											class="rounded-md bg-blue-600 px-2.5 py-1 text-[11px] font-semibold text-white hover:bg-blue-500 disabled:opacity-40"
											:disabled="!agent.name || !getAgentFileState(agent.name).selectedFile || getAgentFileState(agent.name).saving"
											@click="saveAgentFile(agent.name)">
											{{ getAgentFileState(agent.name).saving ? 'Saving…' : 'Save' }}
										</button>
										<button type="button"
											class="rounded-md border border-red-200 px-2 py-1 text-[11px] font-medium text-red-600 hover:bg-red-50 disabled:opacity-40 dark:border-red-900 dark:text-red-400 dark:hover:bg-red-950"
											:disabled="!agent.name || !canDeleteAgentFile(getAgentFileState(agent.name).selectedFile) || getAgentFileState(agent.name).deleting"
											@click="promptDeleteAgentFile(agent.name)">
											{{ getAgentFileState(agent.name).deleting ? 'Deleting…' : 'Delete' }}
										</button>
									</div>
								</div>
								<div class="grid grid-cols-[160px_minmax(0,1fr)] gap-3 sm:grid-cols-[180px_minmax(0,1fr)]">
									<div class="space-y-2 self-start">
										<div class="rounded-lg border border-gray-200 p-1 dark:border-gray-700">
											<div v-if="getAgentFileState(agent.name).files.length" class="space-y-1">
												<button v-for="file in getAgentFileState(agent.name).files" :key="file" type="button"
													class="flex w-full items-center justify-between rounded-md px-2 py-1.5 text-left text-xs font-medium"
													:class="getAgentFileState(agent.name).selectedFile === file ? 'bg-gray-900 text-white dark:bg-white dark:text-gray-900' : 'text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-800'"
													@click="selectAgentFile(agent.name, file)">
													<span class="truncate">{{ file }}</span>
													<span v-if="isProtectedAgentFile(file)"
														class="ml-2 shrink-0 rounded bg-gray-200 px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-gray-700 dark:bg-gray-700 dark:text-gray-200">Built-in</span>
												</button>
											</div>
											<p v-else class="px-2 py-3 text-xs text-gray-500 dark:text-gray-400">
												{{ agent.name ? 'No root markdown files yet. Refresh or add one.' : 'Name the agent first to manage files.' }}
											</p>
										</div>
										<div class="space-y-1">
											<div class="flex gap-1.5">
												<input v-model="getAgentFileState(agent.name).draftFileName" type="text"
													class="field-input py-1 font-mono text-xs"
													:disabled="!agent.name || getAgentFileState(agent.name).creating" placeholder="IDENTITY.md" />
												<button type="button"
													class="rounded-md border border-gray-200 px-2.5 py-1 text-sm font-semibold text-gray-700 hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800 disabled:opacity-40"
													:disabled="!agent.name || getAgentFileState(agent.name).creating"
													@click="createAgentFile(agent.name)">
													{{ getAgentFileState(agent.name).creating ? '…' : '+' }}
												</button>
											</div>
											<button type="button"
												class="w-full rounded-md border border-gray-200 px-2 py-1 text-[11px] text-gray-700 hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800 disabled:opacity-40"
												:disabled="!agent.name || getAgentFileState(agent.name).syncing"
												@click="syncAgentTemplates(agent.name)">
												{{ getAgentFileState(agent.name).syncing ? 'Syncing…' : 'Sync Templates' }}
											</button>
											<p class="text-[11px] leading-4 text-gray-400 dark:text-gray-500">Root-level <span
													class="font-mono">.md</span> only. <span class="font-mono">AGENTS.md</span>, <span class="font-mono">SYSTEM.md</span>, <span
													class="font-mono">MEMORY.md</span>, and <span class="font-mono">RULES.md</span> are protected.
											</p>
										</div>
									</div>

									<div class="relative flex flex-col">
										<textarea :value="getAgentFileState(agent.name).content"
											@input="getAgentFileState(agent.name).content = ($event.target as HTMLTextAreaElement).value"
											@keydown="(e: KeyboardEvent) => { if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 's') { e.preventDefault(); e.stopPropagation(); void saveAgentFile(agent.name); } }"
											class="field-input min-h-[50vh] resize-y py-2 font-mono text-xs"
											:disabled="!agent.name || !getAgentFileState(agent.name).selectedFile"
											:placeholder="agent.name ? 'Select or add a markdown file to edit.' : 'Name the agent first to manage files.'" />
										<p v-if="getAgentFileState(agent.name).error" class="text-xs text-red-600 dark:text-red-400">
											{{ getAgentFileState(agent.name).error }}
										</p>
									</div>
								</div>
							</div>
						</div>

						<!-- Permissions subtab -->
						<div v-show="selectedAgentSubtab === 'permissions'" class="min-h-[60vh] space-y-4 p-5">
							<div class="grid gap-3 lg:max-w-xl">
								<div>
									<label class="field-label" :for="`tool-preset-${agent.name || i}`">Tool preset</label>
									<FancySelect :id="`tool-preset-${agent.name || i}`" :model-value="agentPermissionsPreset(agent)"
										:options="PERMISSION_PRESET_OPTIONS.map((option) => ({
											value: option.value,
											label: option.label,
											caption: option.description,
										}))
											" @update:model-value="updateAgentPermissionsPreset(agent, $event)" />
									<p class="mt-1 text-xs text-gray-400 dark:text-gray-500">
										{{
											PERMISSION_PRESET_OPTIONS.find(
												(option) => option.value === agentPermissionsPreset(agent),
											)?.description
										}}
									</p>
								</div>
							</div>

							<div class="mt-4 space-y-1.5">
								<label class="inline-flex cursor-pointer items-center gap-2">
									<input type="checkbox" :checked="hasToolRestriction(agent)"
										class="h-4 w-4 rounded border-gray-300 text-blue-600 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800"
										@change="setToolRestriction(agent, ($event.target as HTMLInputElement).checked)" />
									<span class="text-sm font-medium text-gray-700 dark:text-gray-300">Restrict tools</span>
								</label>
								<p class="pl-6 text-xs leading-5 text-gray-400 dark:text-gray-500">
									When checked, only the selected tools are visible to this agent.
								</p>
							</div>

							<div v-if="hasToolRestriction(agent) && toolGroupEntries.length" class="mt-3 space-y-2">
								<div v-for="[cat, catTools] in toolGroupEntries" :key="cat"
									class="rounded-lg border border-gray-200 p-3 dark:border-gray-700"
									:class="!isAgentCategoryAccessible(agent, cat) ? 'opacity-50' : ''"
									:data-testid="`agent-tool-group-${agent.name || i}-${cat}`">
									<label class="mb-2 flex cursor-pointer items-center gap-2">
										<input type="checkbox" :checked="isCategoryFullyEnabled(agent, cat)"
											:indeterminate="isCategoryPartiallyEnabled(agent, cat)"
											:disabled="!isAgentCategoryAccessible(agent, cat)"
											:data-testid="`agent-tool-group-checkbox-${agent.name || i}-${cat}`"
											class="h-4 w-4 rounded border-gray-300 text-blue-600 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800"
											@change="toggleCategory(agent, cat, ($event.target as HTMLInputElement).checked)" />
										<span class="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
											{{ toolCategoryLabel(cat) }}
										</span>
									</label>
									<div class="flex flex-wrap gap-x-5 gap-y-1.5 pl-6">
										<label v-for="tool in catTools" :key="tool.name" class="flex cursor-pointer items-center gap-1.5"
											:class="!isAgentToolAccessible(agent, tool.name) ? 'opacity-50' : ''">
											<input type="checkbox" :checked="isToolEnabled(agent, tool.name)"
												:disabled="!isAgentToolAccessible(agent, tool.name)"
												:data-testid="`agent-tool-checkbox-${agent.name || i}-${tool.name}`"
												class="h-3.5 w-3.5 rounded border-gray-300 text-blue-600 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800"
												@change="toggleTool(agent, tool.name, ($event.target as HTMLInputElement).checked)" />
											<span class="font-mono text-xs text-gray-700 dark:text-gray-300">{{ tool.name }}</span>
										</label>
									</div>
								</div>
							</div>

							<p v-if="hasToolRestriction(agent) && !toolGroupEntries.length"
								class="mt-2 text-xs text-gray-400 dark:text-gray-500">
								No tools found. The server may not be reachable.
							</p>

							<div class="mt-4">
								<label class="field-label">Disabled tools</label>
								<ModelSelector :model-value="agent.permissions?.disabled_tools ?? []"
									:options="availableToolNamesForAgent(agent)" multiple
									placeholder="Exclude tools after restrict tools…" empty-text="No matching tools found"
									@update:model-value="
										agent.permissions = {
											...(agent.permissions ?? {}),
											disabled_tools: Array.isArray($event) ? $event : [],
										}
										" />
								<p class="mt-1 text-xs text-gray-400 dark:text-gray-500">
									Applied after the inclusive tool list. Disabled tools always win.
								</p>
							</div>

							<div class="mt-4">
								<button type="button" :data-testid="`agent-tool-permissions-inspect-${agent.name || i}`"
									class="rounded-lg border border-gray-200 px-3 py-2 text-xs font-medium text-gray-700 hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800"
									@click="openToolInspectionModal(agentInspectionTitle(agent, i), agentToolResolution(agent))">Inspect
									tool permissions</button>
							</div>

							<div class="mt-4">
								<label class="field-label">Filesystem Allowed Paths</label>
								<textarea :value="agentFilesystemAllowedPaths(agent)" class="field-input min-h-24 font-mono text-xs"
									placeholder="@/**&#10;!@/token&#10;./docs/**"
									@change="setAgentFilesystemAllowedPaths(agent, $event)" />
								<p class="mt-1 text-xs text-gray-400 dark:text-gray-500">
									One rule per line. Rules are ordered; prefix with <code>!</code> to deny. <code>~</code> means home,
									<code>@</code> means Aviary config dir.
								</p>
							</div>

							<div class="mt-4 space-y-3">
								<div>
									<label class="field-label">Allowed Exec Commands</label>
									<textarea :value="agentExecAllowedCommands(agent)" class="field-input min-h-24 font-mono text-xs"
										placeholder="git status&#10;npm test&#10;python *.py&#10;!rm *"
										@change="setAgentExecAllowedCommands(agent, $event)" />
									<p class="mt-1 text-xs text-gray-400 dark:text-gray-500">
										One ordered glob rule per line. Prefix with <code>!</code> to deny. The exec tool is unavailable
										unless at least one allow rule is configured.
									</p>
								</div>

								<div class="space-y-3">
									<div>
										<label class="field-label">Exec Shell Override (optional)</label>
										<input :value="agent.permissions?.exec?.shell ?? ''" type="text"
											class="field-input font-mono text-xs" :placeholder="execShellPlaceholder"
											@change="setAgentExecShell(agent, $event)" />
										<p class="mt-1 text-xs text-gray-400 dark:text-gray-500">
											Overrides the shell used by the exec tool when command execution is permitted.
										</p>
									</div>
									<label class="flex cursor-pointer items-center gap-2 text-xs text-gray-600 dark:text-gray-400">
										<input :checked="Boolean(agent.permissions?.exec?.shell_interpolate)" type="checkbox"
											class="h-3.5 w-3.5 rounded border-gray-300 text-blue-600 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800"
											@change="setAgentExecShellInterpolate(agent, ($event.target as HTMLInputElement).checked)" />
										Enable shell interpolation
									</label>
								</div>
							</div>
						</div>

						<!-- Channels subtab -->
						<div v-show="selectedAgentSubtab === 'channels'" class="min-h-[60vh] space-y-4 p-5">
							<div class="flex items-center justify-between">
								<h4 class="text-sm font-semibold text-gray-800 dark:text-gray-200">Channels</h4>
								<button type="button"
									class="rounded-lg border border-gray-200 px-3 py-1.5 text-xs text-gray-700 hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800"
									@click="addChannel(i)">+ Add Channel</button>
							</div>

							<div v-if="!agent.channels?.length"
								class="rounded-lg border border-dashed border-gray-300 px-3 py-2 text-xs text-gray-500 dark:border-gray-700 dark:text-gray-400">
								No channels configured for this agent.
							</div>

							<div v-for="(ch, k) in agent.channels" :key="`ch-${i}-${k}`"
								class="space-y-3 rounded-lg border p-4 transition" :class="channelCardClass(ch)">
								<div class="flex flex-wrap items-center justify-between gap-3">
									<div class="flex items-center gap-2">
										<h5 class="text-sm font-semibold text-gray-800 dark:text-gray-200">Channel {{ k + 1 }}</h5>
										<span :class="statusBadgeClass(isChannelEnabled(ch))">
											{{ isChannelEnabled(ch) ? "Enabled" : "Disabled" }}
										</span>
									</div>
									<div class="flex items-center gap-2">
										<SwitchRoot
											:checked="isChannelEnabled(ch)"
											@update:checked="(v) => setChannelEnabled(ch, v)"
											aria-label="Toggle channel enabled"
											class="relative inline-flex h-6 w-11 shrink-0 items-center rounded-full transition-colors focus:outline-none"
											:class="isChannelEnabled(ch) ? 'bg-blue-600' : 'bg-gray-200 dark:bg-gray-700'"
										>
											<SwitchThumb
												class="inline-block h-5 w-5 transform rounded-full bg-white shadow transition-transform"
												:class="isChannelEnabled(ch) ? 'translate-x-[110%]' : 'translate-x-[10%]'"
											/>
										</SwitchRoot>
										<button type="button" class="danger-btn" @click="removeChannel(i, k)">Remove</button>
									</div>
								</div>
								<p v-if="!isChannelEnabled(ch)" class="text-xs text-gray-500 dark:text-gray-400">
									Disabled channels are not started and will not receive or send messages until re-enabled.
								</p>
								<div class="grid gap-3 lg:grid-cols-[160px_1fr]">
									<div>
										<label class="field-label">Type</label>
										<select v-model="ch.type" class="field-input">
											<option value="slack">slack</option>
											<option value="discord">discord</option>
											<option value="signal">signal</option>
										</select>
									</div>
									<div>
										<label class="field-label">{{ channelPrimaryLabel(ch) }}</label>
										<input
											v-model="ch.primary"
											type="text"
											class="field-input"
											:placeholder="channelPrimaryPlaceholder(ch)"
										/>
										<p class="mt-1 text-xs text-gray-400 dark:text-gray-500">
											{{ channelPrimaryHelp(ch) }}
										</p>
									</div>
								</div>

								<div class="grid gap-3 lg:grid-cols-2">
									<div>
										<label class="field-label">Channel model override (optional)</label>
										<ModelSelector :model-value="ch.model ?? ''" :options="availableModelOptions"
											placeholder="Default agent model"
											@update:model-value="ch.model = typeof $event === 'string' ? ($event || undefined) : undefined" />
									</div>
									<div>
										<label class="field-label">Channel fallback overrides (optional)</label>
										<ModelSelector :model-value="ch.fallbacks ?? []" :options="availableModelOptions" multiple
											placeholder="Default agent fallbacks"
											@update:model-value="ch.fallbacks = Array.isArray($event) ? $event : []" />
									</div>
								</div>

								<div>
									<label class="field-label">Channel disabled tools (optional)</label>
									<ModelSelector :model-value="ch.disabled_tools ?? []" :options="availableToolNamesForAgent(agent)"
										multiple placeholder="Exclude tools for this channel…" empty-text="No matching tools found"
										@update:model-value="ch.disabled_tools = Array.isArray($event) ? $event : []" />
									<p class="mt-1 text-xs text-gray-400 dark:text-gray-500">
										Applied after any restrict-tools allow list for messages on this channel.
									</p>
									<div class="mt-3">
										<button type="button" :data-testid="`channel-tool-permissions-inspect-${agent.name || i}-${k}`"
											class="rounded-lg border border-gray-200 px-3 py-2 text-xs font-medium text-gray-700 hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800"
											@click="openToolInspectionModal(channelInspectionTitle(agent, i, ch, k), channelToolResolution(agent, ch))">Inspect
											tool permissions</button>
									</div>
								</div>

								<!-- Allow From entries -->
								<div class="space-y-2">
									<div class="flex items-center justify-between">
										<span class="field-label">Allow From</span>
										<button type="button"
											class="rounded border border-gray-200 px-2 py-1 text-xs text-gray-700 hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800"
											@click="addAllowFrom(i, k)">+ Add Entry</button>
									</div>
									<div v-if="!ch.allow_from?.length"
										class="rounded border border-dashed border-gray-300 px-3 py-2 text-xs text-gray-500 dark:border-gray-700 dark:text-gray-400">
										No entries — all messages will be rejected.
									</div>
									<div v-for="(entry, ei) in ch.allow_from" :key="`af-${i}-${k}-${ei}`"
										class="space-y-2 rounded border p-3 transition" :class="allowFromCardClass(entry)">
										<div class="flex flex-wrap items-center justify-between gap-3">
											<div class="flex items-center gap-2">
												<span
													class="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Entry
													{{ ei + 1 }}</span>
												<span :class="statusBadgeClass(isAllowFromEnabled(entry))">
													{{ isAllowFromEnabled(entry) ? "Enabled" : "Disabled" }}
												</span>
											</div>
											<div class="flex items-center gap-2">
												<SwitchRoot
													:checked="isAllowFromEnabled(entry)"
													@update:checked="(v) => setAllowFromEnabled(entry, v)"
													aria-label="Toggle allow-from entry enabled"
													class="relative inline-flex h-6 w-11 shrink-0 items-center rounded-full transition-colors focus:outline-none"
													:class="isAllowFromEnabled(entry) ? 'bg-blue-600' : 'bg-gray-200 dark:bg-gray-700'"
												>
													<SwitchThumb
														class="inline-block h-5 w-5 transform rounded-full bg-white shadow transition-transform"
														:class="isAllowFromEnabled(entry) ? 'translate-x-[110%]' : 'translate-x-[10%]'"
													/>
												</SwitchRoot>
												<button type="button" class="danger-btn" @click="removeAllowFrom(i, k, ei)">Remove</button>
											</div>
										</div>
										<p v-if="!isAllowFromEnabled(entry)" class="text-xs text-gray-500 dark:text-gray-400">
											Disabled entries are ignored when Aviary checks who can message this agent.
										</p>
										<div class="grid gap-2 lg:grid-cols-[1fr_auto]">
											<div>
												<label class="field-label">From (*, user ID, phone number — comma-separated)</label>
												<input v-model="entry.from" type="text" class="field-input" placeholder="*, +15551234567" />
											</div>
										</div>
										<div class="grid gap-2 lg:grid-cols-2">
											<div>
												<label class="field-label">Allowed Groups (* or specific group IDs, comma-separated)</label>
												<input v-model="entry.allowed_groups" type="text" class="field-input"
													placeholder="Leave empty for DMs only, * for any group" />
											</div>
											<div></div>
										</div>
										<div class="grid gap-2 lg:grid-cols-2">
											<div>
												<label class="field-label">Mention Prefixes (comma-separated, case-insensitive)</label>
												<input :value="entryMentionPrefixes(entry)" type="text" class="field-input"
													placeholder="@bot, !help" @change="setEntryMentionPrefixes(entry, $event)" />
											</div>
											<div>
												<label class="field-label">Exclude Prefixes (comma-separated, case-insensitive)</label>
												<input :value="entryExcludePrefixes(entry)" type="text" class="field-input" placeholder="!, /"
													@change="setEntryExcludePrefixes(entry, $event)" />
											</div>
										</div>
										<div class="space-y-2 pt-1">
											<label class="block cursor-pointer">
												<div class="flex items-center gap-2">
													<input type="checkbox" v-model="entry.respond_to_mentions"
														class="h-3.5 w-3.5 shrink-0 rounded border-gray-300 text-blue-600 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800" />
													<span class="text-xs font-medium text-gray-700 dark:text-gray-300">Respond to @mentions</span>
												</div>
												<p class="mt-0.5 pl-5 text-[11px] leading-4 text-gray-400 dark:text-gray-500">Respond when the
													bot is directly @mentioned in a group chat or DM.</p>
											</label>
											<label class="block cursor-pointer">
												<div class="flex items-center gap-2">
													<input type="checkbox" :checked="entry.mention_prefix_group_only !== false"
														class="h-3.5 w-3.5 shrink-0 rounded border-gray-300 text-blue-600 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800"
														@change="entry.mention_prefix_group_only = ($event.target as HTMLInputElement).checked ? undefined : false" />
													<span class="text-xs font-medium text-gray-700 dark:text-gray-300">Group chats only</span>
												</div>
												<p class="mt-0.5 pl-5 text-[11px] leading-4 text-gray-400 dark:text-gray-500">Mention prefix and
													@mention filters only apply in group chats. DMs from allowed senders are always forwarded.</p>
											</label>
											<label class="block cursor-pointer">
												<div class="flex items-center gap-2">
													<input type="checkbox" :checked="entry.mention_prefix_group_only === false"
														class="h-3.5 w-3.5 shrink-0 rounded border-gray-300 text-blue-600 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800"
														@change="entry.mention_prefix_group_only = ($event.target as HTMLInputElement).checked ? false : undefined" />
													<span class="text-xs font-medium text-gray-700 dark:text-gray-300">Require mention prefix in
														DMs</span>
												</div>
												<p class="mt-0.5 pl-5 text-[11px] leading-4 text-gray-400 dark:text-gray-500">Agent only
													responds to direct messages if the message matches a mention prefix or @mention rule.</p>
											</label>
										</div>
										<div class="grid gap-2 lg:grid-cols-2">
											<div>
												<label class="field-label">Model override (optional)</label>
												<ModelSelector :model-value="entry.model ?? ''" :options="availableModelOptions"
													placeholder="Default agent model"
													@update:model-value="entry.model = typeof $event === 'string' ? ($event || undefined) : undefined" />
											</div>
											<div>
												<label class="field-label">Fallback overrides (optional)</label>
												<ModelSelector :model-value="entry.fallbacks ?? []" :options="availableModelOptions" multiple
													placeholder="Default agent fallbacks"
													@update:model-value="entry.fallbacks = Array.isArray($event) ? $event : []" />
											</div>
										</div>
										<!-- Restrict Tools -->
										<div>
											<div class="mt-2 space-y-1.5">
												<div class="flex flex-wrap items-center gap-3">
													<label class="inline-flex cursor-pointer items-center gap-2">
														<input type="checkbox" :checked="hasEntryToolRestriction(entry)"
															class="h-4 w-4 rounded border-gray-300 text-blue-600 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800"
															@change="setEntryToolRestriction(entry, ($event.target as HTMLInputElement).checked)" />
														<span class="text-sm font-medium text-gray-700 dark:text-gray-300">Restrict tools</span>
													</label>
													<button type="button"
														:data-testid="`entry-tool-permissions-inspect-${agent.name || i}-${k}-${ei}`"
														class="rounded-lg border border-gray-200 px-2.5 py-1.5 text-xs font-medium text-gray-700 hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800"
														@click="openToolInspectionModal(entryInspectionTitle(agent, i, ch, k, entry, ei), entryToolResolution(agent, ch, entry))">Inspect
														tool permissions</button>
												</div>
												<p class="pl-6 text-xs leading-5 text-gray-400 dark:text-gray-500">When checked, only the
													selected tools are available for this entry (overrides agent defaults).</p>
											</div>
											<div v-if="hasEntryToolRestriction(entry) && toolGroupEntries.length" class="mt-3 space-y-2">
												<div v-for="[cat, catTools] in toolGroupEntries" :key="cat"
													class="rounded-lg border border-gray-200 p-3 dark:border-gray-700" :class="draft.agents[i] && !isAgentCategoryAccessible(draft.agents[i], cat)
														? 'opacity-50'
														: ''
														">
													<label class="mb-2 flex cursor-pointer items-center gap-2">
														<input type="checkbox" :checked="isEntryCategoryFullyEnabled(entry, cat)"
															:indeterminate="isEntryCategoryPartiallyEnabled(entry, cat)"
															:disabled="draft.agents[i] && !isAgentCategoryAccessible(draft.agents[i], cat)"
															class="h-4 w-4 rounded border-gray-300 text-blue-600 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800"
															@change="toggleEntryCategory(entry, cat, ($event.target as HTMLInputElement).checked)" />
														<span
															class="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
															{{ toolCategoryLabel(cat) }}
														</span>
													</label>
													<div class="flex flex-wrap gap-x-5 gap-y-1.5 pl-6">
														<label v-for="tool in catTools" :key="tool.name"
															class="flex cursor-pointer items-center gap-1.5" :class="draft.agents[i] && !isAgentToolAccessible(draft.agents[i], tool.name)
																? 'opacity-50'
																: ''
																">
															<input type="checkbox" :checked="isEntryToolEnabled(entry, tool.name)"
																:disabled="draft.agents[i] && !isAgentToolAccessible(draft.agents[i], tool.name)"
																:data-testid="`entry-tool-checkbox-${agent.name || i}-${k}-${ei}-${tool.name}`"
																class="h-3.5 w-3.5 rounded border-gray-300 text-blue-600 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800"
																@change="toggleEntryTool(entry, tool.name, ($event.target as HTMLInputElement).checked)" />
															<span class="font-mono text-xs text-gray-700 dark:text-gray-300">{{ tool.name }}</span>
														</label>
													</div>
												</div>
											</div>
											<p v-if="hasEntryToolRestriction(entry) && !toolGroupEntries.length"
												class="mt-2 text-xs text-gray-400 dark:text-gray-500">
												No tools found. The server may not be reachable.
											</p>
										</div>
									</div>
								</div>

								<div v-if="ch.type === 'slack'" class="grid gap-3 lg:grid-cols-2">
									<div>
										<label class="field-label">Integration ID</label>
										<input v-model="ch.id" type="text" class="field-input" placeholder="workspace-bot" />
									</div>
									<div>
										<label class="field-label">App-Level Token (xapp-…)</label>
										<input v-model="ch.url" type="text" class="field-input" placeholder="xapp-..." />
									</div>
									<div>
										<label class="field-label">Bot Token (xoxb-…)</label>
										<input v-model="ch.token" type="text" class="field-input" placeholder="xoxb-..." />
									</div>
									<div class="lg:col-span-2 rounded-lg border border-gray-200 bg-gray-50/80 p-3 dark:border-gray-700 dark:bg-gray-950/40">
										<div class="flex flex-wrap items-center justify-between gap-3">
											<div>
												<div class="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Workspace Channels</div>
												<p class="mt-1 text-xs text-gray-500 dark:text-gray-400">Use your bot token to validate the Slack workspace and load visible channels for task routing.</p>
											</div>
											<button
												type="button"
												class="rounded-lg border border-gray-200 px-3 py-2 text-xs text-gray-700 hover:bg-gray-100 disabled:opacity-40 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800"
												:disabled="!ch.token || slackWorkspaceState(i, k).loading"
												@click="browseSlackChannels(i, k, ch)"
											>
												{{ slackWorkspaceState(i, k).loading ? "Loading…" : "Browse Channels" }}
											</button>
										</div>
										<p v-if="slackWorkspaceState(i, k).error" class="mt-3 text-xs text-red-600 dark:text-red-400">
											{{ slackWorkspaceState(i, k).error }}
										</p>
										<p v-else-if="slackWorkspaceState(i, k).result" class="mt-3 text-xs text-gray-500 dark:text-gray-400">
											Connected to {{ slackWorkspaceState(i, k).result?.team_name || "Slack" }} with {{ slackVisibleChannels(i, k).length }} visible channels.
										</p>
										<div v-if="slackVisibleChannels(i, k).length" class="mt-3 max-h-48 space-y-2 overflow-y-auto rounded-lg border border-gray-200 bg-white p-2 dark:border-gray-700 dark:bg-gray-900">
											<div
												v-for="workspaceChannel in slackVisibleChannels(i, k)"
												:key="workspaceChannel.id"
												class="flex flex-wrap items-center justify-between gap-2 rounded-md border border-gray-100 px-3 py-2 dark:border-gray-800"
											>
												<div class="min-w-0">
													<div class="truncate text-sm font-medium text-gray-800 dark:text-gray-100">
														#{{ workspaceChannel.name }}
													</div>
													<div class="font-mono text-[11px] text-gray-500 dark:text-gray-400">
														{{ workspaceChannel.id }}
													</div>
												</div>
												<div class="flex flex-wrap items-center gap-1.5 text-[11px] text-gray-500 dark:text-gray-400">
													<span v-if="workspaceChannel.is_private" class="rounded-full bg-gray-100 px-2 py-0.5 dark:bg-gray-800">Private</span>
													<span v-if="workspaceChannel.is_member" class="rounded-full bg-emerald-50 px-2 py-0.5 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300">Joined</span>
													<span v-if="workspaceChannel.num_members" class="rounded-full bg-gray-100 px-2 py-0.5 dark:bg-gray-800">{{ workspaceChannel.num_members }} members</span>
												</div>
											</div>
										</div>
										<p v-else-if="slackWorkspaceState(i, k).result" class="mt-3 text-xs text-gray-500 dark:text-gray-400">
											No visible channels were returned for this bot token.
										</p>
									</div>
								</div>

								<div v-if="ch.type === 'discord'" class="grid gap-3 lg:grid-cols-2">
									<div>
										<label class="field-label">Channel ID</label>
										<input v-model="ch.id" type="text" class="field-input" placeholder="server-bot" />
									</div>
									<div>
										<label class="field-label">Bot Token</label>
										<input v-model="ch.token" type="text" class="field-input" placeholder="Discord bot token" />
									</div>
								</div>

								<div v-if="ch.type === 'signal'" class="grid gap-3 lg:grid-cols-2">
									<div>
										<label class="field-label">Channel ID (E.164)</label>
										<input v-model="ch.id" type="text" class="field-input" placeholder="+15551234567" />
									</div>
									<div>
										<label class="field-label">signal-cli Daemon Address</label>
										<input v-model="ch.url" type="text" class="field-input" placeholder="127.0.0.1:7583" />
									</div>
								</div>

								<div class="flex flex-wrap gap-4 pt-1">
									<label class="flex cursor-pointer items-center gap-2 text-xs text-gray-600 dark:text-gray-400">
										<input type="checkbox" v-model="ch.show_typing"
											class="h-3.5 w-3.5 rounded border-gray-300 text-blue-600 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800" />
										Show typing indicator
									</label>
									<label class="flex cursor-pointer items-center gap-2 text-xs text-gray-600 dark:text-gray-400">
										<input type="checkbox" v-model="ch.reply_to_replies"
											class="h-3.5 w-3.5 rounded border-gray-300 text-blue-600 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800" />
										Reply to replies
									</label>
									<label class="flex cursor-pointer items-center gap-2 text-xs text-gray-600 dark:text-gray-400">
										<input type="checkbox" v-model="ch.react_to_emoji"
											class="h-3.5 w-3.5 rounded border-gray-300 text-blue-600 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800" />
										React to emojis
									</label>
									<label class="flex cursor-pointer items-center gap-2 text-xs text-gray-600 dark:text-gray-400">
										<input type="checkbox" v-model="ch.send_read_receipts"
											class="h-3.5 w-3.5 rounded border-gray-300 text-blue-600 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800" />
										Send read receipts
									</label>
								</div>
								<div class="flex items-center gap-2 pt-1">
									<label class="text-xs text-gray-600 dark:text-gray-400">Group chat history:</label>
									<input type="number" v-model.number="ch.group_chat_history" min="-1" step="1" placeholder="50"
										class="w-20 rounded border border-gray-300 bg-white px-2 py-1 text-xs text-gray-800 focus:outline-none focus:ring-1 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-200" />
									<span class="text-xs text-gray-500 dark:text-gray-500">messages (0 = default 50, -1 = disabled)</span>
								</div>
							</div>
						</div>

						<!-- Tasks subtab -->
						<div v-show="selectedAgentSubtab === 'tasks'" class="min-h-[60vh] space-y-4 p-5">
							<div class="flex items-center justify-between">
								<h4 class="text-sm font-semibold text-gray-800 dark:text-gray-200">Tasks</h4>
							</div>


							<div v-if="!agent.tasks?.length"
								class="rounded-lg border border-dashed border-gray-300 px-3 py-2 text-xs text-gray-500 dark:border-gray-700 dark:text-gray-400">
								No tasks configured for this agent.
							</div>

<div class="grid grid-cols-1 gap-3 lg:grid-cols-[260px_minmax(0,1fr)]">
<!-- Left: list of tasks -->
<div class="min-w-0">
<div class="rounded-lg border border-gray-200 p-1 dark:border-gray-700">
<div v-if="agent.tasks?.length" class="space-y-1">
<div v-for="(task, j) in agent.tasks" :key="`task-button-${i}-${j}`" class="flex items-center gap-1">
<button type="button" class="min-w-0 flex-1 rounded-md px-2 py-1.5 text-left text-xs font-medium" :class="selectedTaskIdx === j ? 'bg-gray-900 text-white dark:bg-white dark:text-gray-900' : 'text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-800'" @click="selectedTaskIdx = j">
<span class="truncate">{{ task.name || `Task ${j + 1}` }}</span>
</button>

<button type="button" class="shrink-0 rounded-md p-1 text-gray-400 hover:bg-gray-100 hover:text-red-600 dark:hover:bg-gray-800" aria-label="Delete task" @click="promptDeleteTask(i, j, task.name)">
<!-- Heroicons outline trash -->
<svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
<path d="M3 6h18" />
<path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6" />
<path d="M10 11v6" />
<path d="M14 11v6" />
<path d="M9 6V4a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2" />
</svg>
</button>

</div>

</div>
<p v-else class="px-2 py-3 text-xs text-gray-500 dark:text-gray-400">No tasks configured for this agent.</p>

<div class="flex gap-1.5 mt-2">
<input v-model="newTaskNameMap[agent.name || i]" type="text" class="field-input py-1 font-mono text-xs" :disabled="!agent.name" placeholder="task-name" />
<button type="button" class="rounded-md border border-gray-200 px-2.5 py-1 text-sm font-semibold text-gray-700 hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800" :disabled="!agent.name" @click="createTaskFromName(i)">+</button>
</div>
</div>
<div class="mt-2"></div>
</div>

<!-- Right: single editor for selected task -->
<div>
<div v-if="selectedTask">
								<div class="mb-3 flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
									<div class="min-w-0">
										<div class="flex flex-wrap items-center gap-2 sm:gap-3">
											<span v-if="!isTaskEnabled(selectedTask)" class="flex-shrink-0 rounded-full bg-red-50 px-3 py-1 text-sm font-medium text-red-700 dark:bg-red-900/20 dark:text-red-300">disabled</span>
											<span class="flex-shrink-0 rounded-full px-3 py-1 text-sm font-medium"
												:class="selectedTask.type === 'script' ? 'bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-300' : 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300'">
												{{ selectedTask.type }}</span>
											<span class="min-w-0 max-w-full rounded-full bg-gray-100 px-3 py-1 text-sm font-medium text-gray-700 dark:bg-gray-800 dark:text-gray-300">
												<span class="block truncate sm:inline">Defined in: {{ taskDefinedIn(selectedTask) }}</span>
											</span>
										</div>
									</div>
									<div class="flex w-full flex-wrap items-center gap-2 sm:w-auto sm:justify-end">
										<button v-if="(!selectedTask.type || selectedTask.type === 'prompt') && selectedTask.prompt" type="button" class="rounded-lg border border-blue-200 px-3 py-2 text-xs font-medium text-blue-600 hover:bg-blue-50 dark:border-blue-800 dark:text-blue-400 dark:hover:bg-blue-950" :disabled="!selectedTask.name" :title="selectedTask.name ? 'Try to compile this prompt task to a Lua script' : 'Task must have a name to convert'" @click="convertTaskToScript(agent.name, selectedTask.name)">Convert to Script</button>
										<label class="inline-flex min-w-0 items-center gap-2">
	<span class="text-sm text-gray-600 dark:text-gray-300">Enabled</span>
	<SwitchRoot
		:checked="isTaskEnabled(selectedTask)"
		@update:checked="setSelectedTaskEnabled"
		aria-label="Toggle task enabled"
		class="relative inline-flex h-6 w-11 shrink-0 items-center rounded-full transition-colors focus:outline-none"
		:class="isTaskEnabled(selectedTask) ? 'bg-blue-600' : 'bg-gray-200 dark:bg-gray-700'"
	>
		<SwitchThumb
			class="inline-block h-5 w-5 transform rounded-full bg-white shadow transition-transform"
			:class="isTaskEnabled(selectedTask) ? 'translate-x-[110%]' : 'translate-x-[10%]'"
		/>
	</SwitchRoot>
</label>
									</div>
								</div>

								<div class="grid gap-3 lg:grid-cols-3">
<div>
<label class="field-label">Task name</label>
<input v-model="selectedTask.name" type="text" class="field-input font-mono" placeholder="daily-briefing"
	@focus="onTaskNameFocus" @blur="onTaskNameBlur" :disabled="renamingTask" />
</div>
<div>
<label class="field-label">Task type</label>
<select v-model="selectedTask.type" class="field-input">
<option value="prompt">Prompt</option>
<option value="script">Script</option>
</select>
</div>
<div>
<label class="field-label">Schedule</label>
<input v-model="selectedTask.schedule" type="text" class="field-input" placeholder="*/5 * * * *" />
</div>
</div>

<div class="grid gap-3 lg:grid-cols-2 mt-3">
<div>
<label class="field-label">Watch</label>
<input v-model="selectedTask.watch" type="text" class="field-input" placeholder="./docs/**/*.md" />
</div>
<div>
<label class="field-label">Send Via</label>
<select :value="taskChannelSelection(selectedTask)" class="field-input" @change="setTaskChannelSelection(selectedTask, $event)">
<option value="">silent</option>
<option v-for="option in configuredTaskChannelOptions(agent)" :key="option.value" :value="option.value">{{ option.label }}</option>
</select>
</div>
</div>

<div v-if="taskChannelSelection(selectedTask)" class="grid gap-3 lg:grid-cols-2 mt-3">
<div>
<label class="field-label">{{ taskDeliveryTargetLabel(agent, selectedTask) }}</label>
<select
	v-if="selectedConfiguredChannel(agent, selectedTask)?.type === 'slack' && slackChannelsForTask(i, agent, selectedTask).length"
	:value="selectedTaskTarget(selectedTask)"
	class="field-input"
	@change="setSelectedTaskTarget(selectedTask, ($event.target as HTMLSelectElement).value)"
>
<option value="">Select a Slack channel…</option>
<option v-for="channel in slackChannelsForTask(i, agent, selectedTask)" :key="channel.id" :value="channel.id">
	#{{ channel.name }} · {{ channel.id }}
</option>
</select>
<input
	v-else
	:value="selectedTaskTarget(selectedTask)"
	type="text"
	class="field-input"
	:placeholder="slackTargetPlaceholder(agent, selectedTask)"
	@input="setSelectedTaskTarget(selectedTask, ($event.target as HTMLInputElement).value)"
/>
</div>
</div>

<div class="mt-4">
<label class="field-label">{{ selectedTask.type === 'script' ? 'Script' : 'Prompt' }}</label>
<textarea v-model="selectedTask.prompt" rows="12" class="field-input min-h-[28vh] font-mono text-xs" :placeholder="selectedTask.type === 'script' ? 'print(\'hello from lua\')' : 'Task prompt...'"></textarea>
<div class="flex gap-2 mt-3">
<!-- Move task to file -->
<button v-if="!selectedTask.from_file" type="button" class="rounded-lg border border-gray-200 px-3 py-2 text-xs font-medium text-gray-600 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-400 dark:hover:bg-gray-800" :disabled="!selectedTask.name" :title="selectedTask.name ? 'Move this task out of aviary.yaml into a tasks/ file' : 'Task must have a name to be moved'" @click="moveTaskToFile(i, selectedTaskIdx ?? 0, agent.name, selectedTask.name)">Move to File</button>
</div>
</div>
</div>
<div v-else class="rounded-lg border border-dashed border-gray-300 px-3 py-6 text-sm text-gray-500 dark:border-gray-700 dark:text-gray-400">
Select a task to edit.
</div>
</div>
</div>
<div class="mt-3 rounded-lg border border-gray-200 bg-gray-50/80 p-4 dark:border-gray-800 dark:bg-gray-950/60">
<label class="flex cursor-pointer items-start gap-3">
<input v-model="draft.scheduler.precompute_tasks" type="checkbox" class="mt-0.5 h-4 w-4 rounded border-gray-300 text-blue-600 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800" />
<span class="text-sm text-gray-700 dark:text-gray-300">Try to precompute tasks
<span class="block text-xs text-gray-500 dark:text-gray-400">Compile deterministic prompt tasks into scripts before scheduling recurring or delayed runs</span>
</span>
</label>
</div>

						</div>
					</div>
				</section>

				<section v-show="activeTab === 'skills'" class="space-y-5 pb-8">
					<div class="flex items-center justify-between">
						<div>
							<h3 class="text-sm font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Installed
								Skills</h3>
							<p class="mt-1 text-xs text-gray-500 dark:text-gray-400">Bundled skills come from the Aviary binary.
								Disk-installed skills come from <code class="font-mono">AVIARY_CONFIG_BASE_DIR/skills</code> and
								<code class="font-mono">~/.agents/skills</code>.</p>
							<p class="mt-2 text-xs text-gray-500 dark:text-gray-400">Search with <code class="font-mono">npx skills find</code>
								or browse <a href="https://skills.sh/" target="_blank" rel="noreferrer"
									class="underline underline-offset-2">skills.sh</a>. Example install:
								<code class="font-mono">npx skills add --global -a universal 4ier/notion-cli</code>.</p>
						</div>
						<button type="button"
							class="rounded-lg border border-gray-200 px-3 py-1.5 text-xs text-gray-700 hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800"
							:disabled="skillsLoading" @click="loadInstalledSkills">
							{{ skillsLoading ? "Loading…" : "Refresh Skills" }}
						</button>
					</div>

					<div v-if="!installedSkills.length"
						class="rounded-xl border border-gray-200 bg-white px-4 py-3 text-sm text-gray-500 dark:border-gray-800 dark:bg-gray-900 dark:text-gray-400">
						No installed skills found.
					</div>

					<div v-for="skill in installedSkills" :key="skill.name"
						class="space-y-4 rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
						<div class="grid gap-3 md:grid-cols-[minmax(0,1fr)_auto] md:items-start">
							<div class="min-w-0">
								<div class="flex flex-wrap items-center gap-2">
									<h4 class="text-base font-semibold text-gray-900 dark:text-white">{{ skill.name }}</h4>
									<span
										class="rounded-full bg-gray-100 px-2 py-0.5 text-[11px] font-medium uppercase tracking-wide text-gray-600 dark:bg-gray-800 dark:text-gray-300">{{ skill.source }}</span>
									<span
										:class="skillConfig(skill.name).enabled ? 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-300' : 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-300'"
										class="rounded-full px-2 py-0.5 text-[11px] font-medium uppercase tracking-wide">
										{{ skillConfig(skill.name).enabled ? "enabled" : "disabled" }}
									</span>
								</div>
								<p v-if="skill.description" class="mt-1 text-sm text-gray-600 dark:text-gray-400">
									{{ skill.description }}
								</p>
								<p class="mt-1 font-mono text-[11px] text-gray-400 dark:text-gray-500">{{ skill.path }}</p>
							</div>

							<label
								class="inline-flex cursor-pointer items-center gap-2 self-start pt-0.5 text-sm text-gray-700 md:justify-self-end dark:text-gray-300">
								<input v-model="skillConfig(skill.name).enabled" type="checkbox"
									class="h-4 w-4 rounded border-gray-300 text-blue-600 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800" />
								Enabled
							</label>
						</div>

						<div v-if="skillSettingEntries(skill).length" class="grid gap-4 lg:grid-cols-2">
							<div v-for="[key, schema] in skillSettingEntries(skill)" :key="`${skill.name}-${key}`">
								<label class="field-label">{{ skillSettingLabel(key, schema) }}</label>
								<input v-if="skillSettingInputKind(schema) === 'string'" :value="skillStringSetting(skill.name, key)"
									type="text" class="field-input" :placeholder="skillSettingPlaceholder(schema)"
									@input="setSkillStringSetting(skill.name, key, $event)" />
								<input v-else :value="skillArraySetting(skill.name, key)" type="text" class="field-input"
									:placeholder="skillSettingPlaceholder(schema)"
									@input="setSkillArraySetting(skill.name, key, $event)" />
								<p v-if="typeof schema.description === 'string' && schema.description"
									class="mt-1 text-xs text-gray-400 dark:text-gray-500">
									{{ schema.description }}
								</p>
							</div>
						</div>
					</div>
				</section>

				<section v-show="activeTab === 'sessions'" class="space-y-5 pb-8">
					<div class="rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
						<h3 class="mb-4 text-sm font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Sessions
						</h3>
						<div class="grid gap-3 lg:grid-cols-[280px_auto_auto]">
							<div>
								<label class="field-label">Agent</label>
								<select v-model="sessionAgent" class="field-input">
									<option value="">Select agent</option>
									<option v-for="agent in draft.agents" :key="`sess-${agent.name}`" :value="agent.name">{{ agent.name }}
									</option>
								</select>
							</div>
							<div class="flex items-end">
								<button type="button"
									class="rounded-lg border border-gray-200 px-3 py-2 text-xs text-gray-700 hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800"
									:disabled="!sessionAgent || sessionLoading"
									@click="loadSessions">{{ sessionLoading ? 'Loading…' : 'Refresh Sessions' }}</button>
							</div>
							<div class="flex items-end">
								<button type="button"
									class="rounded-lg bg-blue-600 px-3 py-2 text-xs font-semibold text-white hover:bg-blue-500 disabled:opacity-50"
									:disabled="!sessionAgent || sessionLoading" @click="createSession">+ Create Session</button>
							</div>
						</div>

						<div class="mt-4 overflow-hidden rounded-lg border border-gray-200 dark:border-gray-700">
							<table v-if="sessions.length" class="w-full text-sm">
								<thead>
									<tr
										class="border-b border-gray-200 text-left text-xs font-medium text-gray-500 dark:border-gray-700 dark:text-gray-400">
										<th class="px-3 py-2">Name</th>
										<th class="px-3 py-2">ID</th>
										<th class="px-3 py-2">Updated</th>
										<th class="px-3 py-2">Actions</th>
									</tr>
								</thead>
								<tbody>
									<tr v-for="s in sessions" :key="s.id"
										class="border-b border-gray-100 text-gray-700 dark:border-gray-800 dark:text-gray-300">
										<td class="px-3 py-2">{{ s.name || '—' }}</td>
										<td class="px-3 py-2 font-mono text-xs text-gray-500 dark:text-gray-400">{{ s.id.slice(-10) }}</td>
										<td class="px-3 py-2 text-xs">{{ formatDate(s.updated_at) }}</td>
										<td class="px-3 py-2">
											<div class="flex items-center gap-2">
												<button type="button" class="danger-btn" :disabled="!s.is_processing"
													:class="!s.is_processing ? 'opacity-40 cursor-not-allowed' : ''"
													@click="stopSession(s.id)">Stop</button>
												<button type="button" class="danger-btn"
													@click="removeTarget = s; removeTargetOpen = true">Remove</button>
											</div>
										</td>
									</tr>
								</tbody>
							</table>
							<div v-else class="px-3 py-3 text-xs text-gray-500 dark:text-gray-400">No sessions found.</div>
						</div>
					</div>
				</section>

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
												:class="entry.authType === 'oauth' ? 'inline-block rounded bg-blue-100 px-1.5 py-0.5 text-xs font-medium text-blue-700 dark:bg-blue-900/30 dark:text-blue-300' : 'inline-block rounded bg-gray-100 px-1.5 py-0.5 text-xs font-medium text-gray-600 dark:bg-gray-800 dark:text-gray-300'">
												{{ entry.authType === 'oauth' ? 'OAuth' : 'API Key' }}
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
											<span v-else class="tracking-widest text-gray-400 dark:text-gray-500">••••••••</span>
										</td>
										<td class="px-3 py-2 text-right">
											<div class="flex items-center justify-end gap-2">
												<button v-if="entry.authType === 'oauth'" type="button"
													class="text-xs text-blue-600 hover:underline disabled:opacity-50 dark:text-blue-400"
													:disabled="oauthBusy" @click="reauthorizeProvider(entry.provider)">Re-authorize</button>
												<button type="button"
													class="text-gray-400 hover:text-red-500 dark:text-gray-500 dark:hover:text-red-400"
													:title="`Remove ${entry.key}`" @click="deleteProviderCredential(entry.key)">
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
								<template v-if="providerAddSelection.endsWith(':apikey')">
									<input v-model="providerApiKeyValue" type="password" class="field-input max-w-[260px]"
										placeholder="API key…" />
									<button type="button"
										class="rounded-lg bg-blue-600 px-3 py-2 text-xs font-semibold text-white hover:bg-blue-500 disabled:opacity-50"
										:disabled="!providerApiKeyValue.trim()" @click="addProviderApiKey">Add</button>
								</template>
								<button v-else type="button"
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

						<!-- GitHub Copilot device-flow inline form -->
						<div v-if="copilotUserCode" class="mt-3 space-y-2 rounded-lg border border-gray-200 p-3 dark:border-gray-700">
							<p class="text-xs text-gray-500 dark:text-gray-400">
								Visit <a :href="copilotVerifyUrl" target="_blank" rel="noreferrer"
									class="text-blue-600 hover:underline dark:text-blue-400">{{ copilotVerifyUrl }}</a>
								and enter this code:
							</p>
							<div class="flex items-center justify-center rounded-md bg-gray-50 py-2 dark:bg-gray-800">
								<span class="font-mono text-lg font-bold tracking-widest text-gray-900 dark:text-white">{{ copilotUserCode }}</span>
							</div>
							<button type="button"
								class="w-full rounded-lg bg-gray-900 px-3 py-2 text-xs font-semibold text-white hover:bg-gray-700 disabled:opacity-50 dark:bg-gray-700 dark:hover:bg-gray-600"
								:disabled="oauthBusy" @click="completeCopilot">
								{{ oauthBusy ? 'Waiting for authorization…' : "I\'ve authorized — Complete" }}
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
							@click="refreshCredentials">↻ Refresh</button>
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

			</div>
		</div>

		<div v-if="toolInspectionModal" class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 px-4 py-6"
			@click.self="closeToolInspectionModal">
			<div
				class="flex max-h-[85vh] w-full max-w-3xl flex-col overflow-hidden rounded-xl border border-gray-200 bg-white shadow-2xl dark:border-gray-800 dark:bg-gray-900">
				<div class="flex items-start justify-between gap-4 border-b border-gray-200 px-5 py-4 dark:border-gray-800">
					<div>
						<h3 class="text-sm font-semibold text-gray-900 dark:text-white">Inspect Tool Permissions</h3>
						<p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ toolInspectionModal.title }}</p>
					</div>
					<button type="button"
						class="rounded-lg border border-gray-200 px-3 py-1.5 text-xs text-gray-700 hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800"
						@click="closeToolInspectionModal">Close</button>
				</div>
				<div class="space-y-4 overflow-y-auto p-5">
					<p class="text-xs leading-5 text-gray-500 dark:text-gray-400">
						Resolution order: preset accessibility, then restrict-tools allow list, then disabled-tools exclusions.
					</p>
					<div class="grid gap-3 sm:grid-cols-4">
						<div class="rounded-lg border border-gray-200 px-3 py-2 dark:border-gray-800">
							<div class="text-[11px] uppercase tracking-wide text-gray-400 dark:text-gray-500">Preset</div>
							<div class="mt-1 text-sm font-semibold text-gray-800 dark:text-gray-200">
								{{ toolInspectionModal.resolution.preset }}
							</div>
						</div>
						<div class="rounded-lg border border-gray-200 px-3 py-2 dark:border-gray-800">
							<div class="text-[11px] uppercase tracking-wide text-gray-400 dark:text-gray-500">Accessible</div>
							<div class="mt-1 text-sm font-semibold text-gray-800 dark:text-gray-200">
								{{ toolInspectionModal.resolution.presetAccessibleTools.length }}
							</div>
						</div>
						<div class="rounded-lg border border-gray-200 px-3 py-2 dark:border-gray-800">
							<div class="text-[11px] uppercase tracking-wide text-gray-400 dark:text-gray-500">Disabled</div>
							<div class="mt-1 text-sm font-semibold text-gray-800 dark:text-gray-200">
								{{ toolInspectionModal.resolution.effectiveDisabledTools.length }}
							</div>
						</div>
						<div class="rounded-lg border border-gray-200 px-3 py-2 dark:border-gray-800">
							<div class="text-[11px] uppercase tracking-wide text-gray-400 dark:text-gray-500">Final</div>
							<div class="mt-1 text-sm font-semibold text-gray-800 dark:text-gray-200">
								{{ toolInspectionModal.resolution.finalTools.length }}
							</div>
						</div>
					</div>
					<pre data-testid="tool-permissions-inspector-output"
						class="max-h-[50vh] overflow-auto rounded-lg bg-gray-950 px-4 py-3 text-xs leading-5 text-gray-100">
					{{ toolInspectionOutput }}
					</pre>
				</div>
			</div>
		</div>
	</AppLayout>

	<!-- Remove agent confirmation dialog -->
	<AlertDialogRoot :open="removeAgentOpen" @update:open="(v) => { if (!v) removeAgentOpen = false }">
		<AlertDialogPortal>
			<AlertDialogOverlay class="fixed inset-0 z-50 bg-black/50" />
			<AlertDialogContent
				class="fixed left-1\2 top-1\2 z-50 w-full max-w-lg -translate-x-1\2 -translate-y-1\2 rounded-xl bg-white p-6 shadow-2xl dark:bg-gray-900">
				<AlertDialogTitle class="text-base font-bold text-gray-900 dark:text-white">
					Remove agent?
				</AlertDialogTitle>
				<AlertDialogDescription class="mt-2 text-sm text-gray-600 dark:text-gray-400">
					This will remove
					<span
						class="font-medium text-gray-900 dark:text-white">{{ removeAgentTarget !== null ? (draft?.agents[removeAgentTarget]?.name || 'this agent') : '' }}</span>
					from the configuration. This cannot be undone.
				</AlertDialogDescription>
				<div ref="removeAgentBtns" class="mt-6 flex justify-end gap-3">
					<AlertDialogCancel
						class="rounded-lg border border-gray-200 px-4 py-2 text-sm text-gray-600 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-400 dark:hover:bg-gray-800">
						Cancel
					</AlertDialogCancel>
					<AlertDialogAction
						class="rounded-lg bg-red-600 px-4 py-2 text-sm font-semibold text-white hover:bg-red-500"
						@click="confirmRemoveAgent">
						Remove
					</AlertDialogAction>
				</div>
			</AlertDialogContent>
		</AlertDialogPortal>
	</AlertDialogRoot>

	<!-- Delete file confirmation dialog -->
	<AlertDialogRoot :open="!!deleteFileTarget" @update:open="(v) => { if (!v) deleteFileTarget = null }">
		<AlertDialogPortal>
			<AlertDialogOverlay class="fixed inset-0 z-50 bg-black/50" />
			<AlertDialogContent
				class="fixed left-1\2 top-1\2 z-50 w-full max-w-lg -translate-x-1\2 -translate-y-1\2 rounded-xl bg-white p-6 shadow-2xl dark:bg-gray-900">
				<AlertDialogTitle class="text-base font-bold text-gray-900 dark:text-white">
					Delete file?
				</AlertDialogTitle>
				<AlertDialogDescription class="mt-2 text-sm text-gray-600 dark:text-gray-400">
					This will permanently delete
					<span class="font-medium text-gray-900 dark:text-white">{{ deleteFileTarget?.file }}</span>.
					This cannot be undone.
				</AlertDialogDescription>
				<div ref="deleteFileBtns" class="mt-6 flex justify-end gap-3">
					<AlertDialogCancel
						class="rounded-lg border border-gray-200 px-4 py-2 text-sm text-gray-600 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-400 dark:hover:bg-gray-800">
						Cancel
					</AlertDialogCancel>
					<AlertDialogAction
						class="rounded-lg bg-red-600 px-4 py-2 text-sm font-semibold text-white hover:bg-red-500"
						@click="confirmDeleteAgentFile">
						Delete
					</AlertDialogAction>
				</div>
			</AlertDialogContent>
		</AlertDialogPortal>
	</AlertDialogRoot>

	<!-- Delete task confirmation dialog -->
	<AlertDialogRoot :open="deleteTaskOpen" @update:open="(v) => { if (!v) deleteTaskOpen = false }">
		<AlertDialogPortal>
			<AlertDialogOverlay class="fixed inset-0 z-50 bg-black/50" />
			<AlertDialogContent class="fixed left-1/2 top-1/2 z-50 w-full max-w-lg -translate-x-1/2 -translate-y-1/2 rounded-xl bg-white p-6 shadow-2xl dark:bg-gray-900">
				<AlertDialogTitle class="text-base font-bold text-gray-900 dark:text-white">Delete task?</AlertDialogTitle>
				<AlertDialogDescription class="mt-2 text-sm text-gray-600 dark:text-gray-400">This will permanently delete <span class="font-medium text-gray-900 dark:text-white">{{ deleteTaskTarget?.name || 'this task' }}</span>. This cannot be undone.</AlertDialogDescription>
				<div ref="deleteTaskBtns" class="mt-6 flex justify-end gap-3">
					<AlertDialogCancel class="rounded-lg border border-gray-200 px-4 py-2 text-sm text-gray-600 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-400 dark:hover:bg-gray-800">Cancel</AlertDialogCancel>
					<AlertDialogAction class="rounded-lg bg-red-600 px-4 py-2 text-sm font-semibold text-white hover:bg-red-500" @click="confirmDeleteTaskAction">Delete</AlertDialogAction>
				</div>
			</AlertDialogContent>
		</AlertDialogPortal>
	</AlertDialogRoot>

	<!-- Remove session confirmation dialog -->
	<AlertDialogRoot :open="removeTargetOpen" @update:open="(v) => { if (!v) removeTargetOpen = false }">
		<AlertDialogPortal>
			<AlertDialogOverlay class="fixed inset-0 z-50 bg-black/50" />
			<AlertDialogContent
				class="fixed left-1/2 top-1/2 z-50 w-full max-w-lg -translate-x-1/2 -translate-y-1/2 rounded-xl bg-white p-6 shadow-2xl dark:bg-gray-900">
				<AlertDialogTitle class="text-base font-bold text-gray-900 dark:text-white">
					Remove session?
				</AlertDialogTitle>
				<AlertDialogDescription class="mt-2 text-sm text-gray-600 dark:text-gray-400">
					This will permanently delete
					<span
						class="break-all font-medium text-gray-900 dark:text-white">{{ removeTarget?.name || removeTarget?.id }}</span>
					and all its messages. This cannot be undone.
				</AlertDialogDescription>
				<div ref="removeSessionBtns" class="mt-6 flex justify-end gap-3">
					<AlertDialogCancel
						class="rounded-lg border border-gray-200 px-4 py-2 text-sm text-gray-600 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-400 dark:hover:bg-gray-800">
						Cancel
					</AlertDialogCancel>
					<AlertDialogAction
						class="rounded-lg bg-red-600 px-4 py-2 text-sm font-semibold text-white hover:bg-red-500"
						@click="confirmRemoveSession">
						Remove
					</AlertDialogAction>
				</div>
			</AlertDialogContent>
		</AlertDialogPortal>
	</AlertDialogRoot>
</template>

<script setup lang="ts">
import {
	AlertDialogAction,
	AlertDialogCancel,
	AlertDialogContent,
	AlertDialogDescription,
	AlertDialogOverlay,
	AlertDialogPortal,
	AlertDialogRoot,
	AlertDialogTitle,
	SwitchRoot,
	SwitchThumb,
} from "radix-vue";
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import AppLayout from "../components/AppLayout.vue";
import FancySelect from "../components/FancySelect.vue";
import ModelSelector from "../components/ModelSelector.vue";
import { useAvailableModels } from "../composables/useAvailableModels";
import { type MCPToolInfo, useMCP } from "../composables/useMCP";
import {
	clampToolNamesForPreset,
	groupTools,
	isToolAccessibleForPreset,
	isToolGroupAccessibleForPreset,
	normalizePermissionsPreset,
	type PermissionsPreset,
	type ResolvedToolPermissions,
	resolveToolPermissions,
	toolCategory,
	toolCategoryLabel,
} from "../lib/toolPermissions";
import { useAuthStore } from "../stores/auth";
import {
	type AgentChannel,
	type AgentEntry,
	type AgentTask,
	type AllowFromEntry,
	type AppConfig,
	type SkillConfig,
	useSettingsStore,
} from "../stores/settings";

type Tab = "general" | "agents" | "skills" | "sessions" | "providers";

interface SessionRow {
	id: string;
	name: string;
	updated_at: string;
	is_processing?: boolean;
}

interface RuntimeAgent {
	name: string;
	model?: string;
	fallbacks?: string[];
}

interface InstalledSkill {
	name: string;
	description: string;
	content: string;
	path: string;
	installed: boolean;
	enabled: boolean;
	source: string;
	settings_schema?: SkillSettingsSchema;
}

interface SkillSettingSchema {
	type?: string;
	title?: string;
	description?: string;
	placeholder?: string;
	items?: SkillSettingSchema;
}

interface SkillSettingsSchema {
	type?: string;
	properties?: Record<string, SkillSettingSchema>;
}

interface TaskChannelOption {
	value: string;
	label: string;
	type: string;
}

interface SlackWorkspaceChannelOption {
	id: string;
	name: string;
	name_normalized?: string;
	is_private?: boolean;
	is_member?: boolean;
	is_archived?: boolean;
	num_members?: number;
}

interface SlackWorkspaceBrowseResult {
	team_id?: string;
	team_name?: string;
	bot_user_id?: string;
	channels: SlackWorkspaceChannelOption[];
}

interface ToolInspectionModalState {
	title: string;
	resolution: ResolvedToolPermissions;
}

function safeJsonParse<T>(raw: string, fallback: T): T {
	const trimmed = raw.trim();
	if (!trimmed) {
		return fallback;
	}
	try {
		const parsed = JSON.parse(trimmed) as T | null;
		return parsed ?? fallback;
	} catch (error) {
		if (error instanceof SyntaxError) {
			return fallback;
		}
		throw error;
	}
}

const route = useRoute();
const router = useRouter();

const tabs: Tab[] = ["general", "agents", "skills", "sessions", "providers"];

function routeToActiveTab(): Tab {
	if (route.path.startsWith("/settings/agents")) return "agents";
	const tab = route.params.tab as Tab | undefined;
	return tab && tabs.includes(tab) ? tab : "general";
}

const activeTab = ref<Tab>(routeToActiveTab());

watch(
	() => route.path,
	() => {
		activeTab.value = routeToActiveTab();
		if (activeTab.value === "sessions") {
			if (!sessionAgent.value && draft.value.agents.length > 0) {
				sessionAgent.value = draft.value.agents[0].name;
			} else if (sessionAgent.value) {
				loadSessions();
			}
		}
	},
);

const store = useSettingsStore();
const { callTool, listTools } = useMCP();
const { availableModelOptions, credentials, refreshCredentials } =
	useAvailableModels();
const authStore = useAuthStore();

// Rename state for tasks
const editingTaskOriginalName = ref<string | null>(null);
const renamingTask = ref(false);
const renameTaskError = ref<string | null>(null);

let settingsWs: WebSocket | null = null;

function connectWs() {
	const protocol = location.protocol === "https:" ? "wss:" : "ws:";
	const tok = authStore.getToken();
	const qs = tok ? `?token=${encodeURIComponent(tok)}` : "";
	settingsWs = new WebSocket(`${protocol}//${location.host}/api/ws${qs}`);
	settingsWs.onmessage = async (e) => {
		try {
			const data = JSON.parse(e.data as string) as {
				type?: string;
				session_id?: string;
				goos?: string;
			};
			if (data.goos) {
				hostGoos.value = data.goos;
			}
			if (
				data.type === "session_message" ||
				data.type === "session_processing"
			) {
				if (activeTab.value === "sessions" && sessionAgent.value) {
					await loadSessions();
				}
			}
		} catch {
			// ignore malformed frames
		}
	};
	settingsWs.onclose = () => {
		settingsWs = null;
	};
}

const loading = ref(false);
const saving = ref(false);
const reverting = ref(false);
const errorMessage = ref("");
const okMessage = ref("");
const compileToastVisible = ref(false);
const slackWorkspaceBrowsers = ref<
	Record<
		string,
		{
			loading: boolean;
			error: string;
			result: SlackWorkspaceBrowseResult | null;
		}
	>
>({});

function slackWorkspaceKey(agentIndex: number, channelIndex: number): string {
	return `${agentIndex}:${channelIndex}`;
}

function slackWorkspaceState(agentIndex: number, channelIndex: number) {
	const key = slackWorkspaceKey(agentIndex, channelIndex);
	if (!slackWorkspaceBrowsers.value[key]) {
		slackWorkspaceBrowsers.value[key] = {
			loading: false,
			error: "",
			result: null,
		};
	}
	return slackWorkspaceBrowsers.value[key];
}
const hostGoos = ref("");
const saveSuccessVisible = ref(false);
const headerNoticeText = ref("Settings saved");
const revertAvailable = ref(false);

const draft = ref<AppConfig>(emptyConfig());

const agentSubtabs = [
	"general",
	"permissions",
	"channels",
	"files",
	"tasks",
] as const;
type AgentSubtab = (typeof agentSubtabs)[number];

// Returns the URL segment for an agent: its name, or "_<index>" if unnamed.
function agentRouteId(idx: number): string {
	return draft.value.agents[idx]?.name || `_${idx}`;
}

// Resolves a URL segment back to an agent index.
function agentIdxFromParam(param: string): number {
	if (param.startsWith("_")) {
		const n = parseInt(param.slice(1), 10);
		return Number.isNaN(n) ? 0 : n;
	}
	const idx = draft.value.agents.findIndex((a) => a.name === param);
	return idx >= 0 ? idx : 0;
}

function agentRoutePath(idx: number, subtab: AgentSubtab): string {
	return `/settings/agents/${agentRouteId(idx)}/${subtab}`;
}

const selectedAgentSubtab = ref<AgentSubtab>(
	agentSubtabs.includes(route.params.subtab as AgentSubtab)
		? (route.params.subtab as AgentSubtab)
		: "general",
);
const selectedAgentIdx = ref(
	route.params.agent ? agentIdxFromParam(route.params.agent as string) : 0,
);

// Selected task index for the currently-open agent (null when none selected)
const selectedTaskIdx = ref<number | null>(
	draft.value.agents[selectedAgentIdx.value]?.tasks?.length ? 0 : null,
);

const selectedTask = computed((): AgentTask | null => {
	const agent = draft.value.agents[selectedAgentIdx.value];
	if (!agent?.tasks?.length) return null;
	const idx = selectedTaskIdx.value ?? 0;
	if (idx < 0 || idx >= agent.tasks.length) return agent.tasks[0] ?? null;
	return agent.tasks[idx] ?? null;
});

// Agent tab click → push new route (also resets subtab) and reset selectedTaskIdx for the new agent.
watch(selectedAgentIdx, (idx) => {
	const target = agentRoutePath(idx, selectedAgentSubtab.value);
	if (route.path !== target) void router.push(target);
	// Reset selectedTaskIdx for the newly-selected agent so a task is shown if available.
	const tasks = draft.value.agents[idx]?.tasks ?? [];
	selectedTaskIdx.value = tasks.length ? 0 : null;
});

// Subtab click → replace current URL segment.
watch(selectedAgentSubtab, (subtab) => {
	if (!route.path.startsWith("/settings/agents")) return;
	const target = agentRoutePath(selectedAgentIdx.value, subtab);
	if (route.path !== target) void router.replace(target);
});

// Agent rename (or new agent getting a name) → replace URL in place.
watch(
	() => draft.value.agents[selectedAgentIdx.value]?.name,
	() => {
		if (!route.path.startsWith("/settings/agents")) return;
		const target = agentRoutePath(
			selectedAgentIdx.value,
			selectedAgentSubtab.value,
		);
		if (route.path !== target) void router.replace(target);
	},
);

// Ensure selectedTaskIdx is set when tasks are loaded or changed (e.g., importAgents).
watch(
	() => draft.value.agents[selectedAgentIdx.value]?.tasks?.length,
	(len) => {
		if (
			len &&
			(selectedTaskIdx.value === null || selectedTaskIdx.value === undefined)
		) {
			selectedTaskIdx.value = 0;
		} else if (!len) {
			selectedTaskIdx.value = null;
		}
	},
);

// Browser back/forward or direct URL navigation → sync index and subtab.
watch(
	() => route.params.agent as string | undefined,
	(param) => {
		if (!param || !draft.value.agents.length) return;
		const idx = agentIdxFromParam(param);
		if (idx !== selectedAgentIdx.value) selectedAgentIdx.value = idx;
	},
);
watch(
	() => route.params.subtab as string | undefined,
	(subtab) => {
		if (subtab && agentSubtabs.includes(subtab as AgentSubtab)) {
			selectedAgentSubtab.value = subtab as AgentSubtab;
		}
	},
);

// /settings/agents with no agent param → redirect to current selection.
watch(
	() => route.path,
	(path) => {
		if (path === "/settings/agents") {
			void router.replace(
				agentRoutePath(selectedAgentIdx.value, selectedAgentSubtab.value),
			);
		}
	},
	{ immediate: true },
);

const selectedAgentAsSingletonList = computed(() =>
	selectedAgentIdx.value < draft.value.agents.length
		? [
				{
					agent: draft.value.agents[selectedAgentIdx.value],
					i: selectedAgentIdx.value,
				},
			]
		: [],
);

const concurrencyInput = ref("");
const serverPortInput = ref("");
const cdpPortInput = ref("");
let saveSuccessTimer: ReturnType<typeof setTimeout> | null = null;
let lastSavedSnapshot = "";

const execShellPlaceholder = computed(() => {
	switch (hostGoos.value) {
		case "windows":
			return "powershell.exe -NoProfile -Command";
		case "darwin":
			return "/bin/zsh -lc";
		case "linux":
			return "/bin/bash -lc";
		default:
			return "/bin/sh -lc";
	}
});

const sessionAgent = ref("");
const sessions = ref<SessionRow[]>([]);
const sessionLoading = ref(false);

watch(sessionAgent, (val) => {
	if (val) loadSessions();
	else sessions.value = [];
});
const removeTarget = ref<SessionRow | null>(null);
const removeTargetOpen = ref(false);
const removeSessionBtns = ref<HTMLElement>();
watch(removeTargetOpen, (v) => {
	if (v)
		setTimeout(() => {
			const btns =
				removeSessionBtns.value?.querySelectorAll<HTMLElement>("button");
			btns?.[btns.length - 1]?.focus();
		});
});

const oauthBusy = ref(false);
const anthropicUrl = ref("");
const anthropicCode = ref("");
const copilotUserCode = ref("");
const copilotVerifyUrl = ref("");
const providerAddSelection = ref("");
const providerApiKeyValue = ref("");
const secretName = ref("");
const secretValue = ref("");

const KNOWN_PROVIDERS = [
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

const configuredProviders = computed(() => {
	const entries: Array<{
		key: string;
		provider: string;
		providerLabel: string;
		authType: "oauth" | "apikey";
	}> = [];
	for (const cred of credentials.value) {
		for (const p of KNOWN_PROVIDERS) {
			if (p.hasOAuth && cred === `${p.authId}:oauth`) {
				entries.push({
					key: cred,
					provider: p.id,
					providerLabel: p.label,
					authType: "oauth",
				});
			} else if (p.hasApiKey && cred === `${p.authId}:default`) {
				entries.push({
					key: cred,
					provider: p.id,
					providerLabel: p.label,
					authType: "apikey",
				});
			}
		}
	}
	return entries;
});

const availableProviderOptions = computed(() => {
	const configured = new Set(configuredProviders.value.map((e) => e.key));
	const options: Array<{ key: string; label: string; provider: string }> = [];
	for (const p of KNOWN_PROVIDERS) {
		if (p.hasOAuth && !configured.has(`${p.authId}:oauth`)) {
			options.push({
				key: `${p.authId}:oauth`,
				label: `${p.label} (OAuth)`,
				provider: p.id,
			});
		}
		if (p.hasApiKey && !configured.has(`${p.authId}:default`)) {
			options.push({
				key: `${p.authId}:apikey`,
				label: `${p.label} (API Key)`,
				provider: p.id,
			});
		}
	}
	return options;
});

const extraSecrets = computed(() => {
	const providerKeys = new Set<string>();
	for (const p of KNOWN_PROVIDERS) {
		providerKeys.add(`${p.authId}:oauth`);
		providerKeys.add(`${p.authId}:default`);
	}
	return credentials.value.filter((cred) => !providerKeys.has(cred));
});

const webSearchSecretOptions = computed(() =>
	extraSecrets.value.filter((cred) => !cred.endsWith(":oauth")),
);

const webSearchSecretSelection = computed({
	get(): string {
		const ref = draft.value.search.web.brave_api_key?.trim() ?? "";
		return ref.startsWith("auth:") ? ref.slice(5) : "";
	},
	set(name: string) {
		draft.value.search.web.brave_api_key = name ? `auth:${name}` : "";
	},
});

const availableTools = ref<MCPToolInfo[]>([]);
const installedSkills = ref<InstalledSkill[]>([]);
const skillsLoading = ref(false);
const toolInspectionModal = ref<ToolInspectionModalState | null>(null);

const toolGroupEntries = computed((): [string, MCPToolInfo[]][] => {
	return groupTools(availableTools.value);
});

const availableToolNames = computed(() =>
	availableTools.value
		.map((tool) => tool.name)
		.sort((a, b) => a.localeCompare(b)),
);
const hasDraftChanges = computed(() => {
	if (loading.value) return false;
	return normalizedDraftSnapshot() !== lastSavedSnapshot;
});

const PERMISSION_PRESET_OPTIONS: Array<{
	value: PermissionsPreset;
	label: string;
	description: string;
}> = [
	{
		value: "full",
		label: "Full",
		description: "No preset cap. Any tool group may be enabled.",
	},
	{
		value: "standard",
		label: "Standard",
		description: "Blocks agent, auth, exec, file, and server tool groups.",
	},
	{
		value: "minimal",
		label: "Minimal",
		description: "Also blocks browser, skills, and usage on top of standard.",
	},
];

function agentPermissionsPreset(agent: AgentEntry): PermissionsPreset {
	return normalizePermissionsPreset(agent.permissions?.preset);
}

function sanitizeAgentToolSelections(agent: AgentEntry) {
	const preset = agentPermissionsPreset(agent);
	if (agent.permissions) {
		agent.permissions.preset =
			preset === "standard" ? undefined : agentPermissionsPreset(agent);
		agent.permissions.tools = clampToolNamesForPreset(
			preset,
			agent.permissions.tools,
		);
		agent.permissions.disabled_tools = clampToolNamesForPreset(
			preset,
			agent.permissions.disabled_tools,
		);
	}
	for (const channel of agent.channels ?? []) {
		channel.disabled_tools = clampToolNamesForPreset(
			preset,
			channel.disabled_tools,
		);
		for (const entry of channel.allow_from ?? []) {
			entry.restrict_tools = clampToolNamesForPreset(
				preset,
				entry.restrict_tools,
			);
		}
	}
}

function updateAgentPermissionsPreset(agent: AgentEntry, value: unknown) {
	const preset = normalizePermissionsPreset(
		typeof value === "string" ? value : undefined,
	);
	agent.permissions = {
		...(agent.permissions ?? {}),
		preset: preset === "standard" ? undefined : preset,
	};
	sanitizeAgentToolSelections(agent);
}

function isAgentToolAccessible(agent: AgentEntry, toolName: string): boolean {
	return isToolAccessibleForPreset(agentPermissionsPreset(agent), toolName);
}

function isAgentCategoryAccessible(
	agent: AgentEntry,
	category: string,
): boolean {
	return isToolGroupAccessibleForPreset(
		agentPermissionsPreset(agent),
		category,
	);
}

function availableToolsForAgent(agent: AgentEntry): MCPToolInfo[] {
	return availableTools.value.filter((tool) =>
		isAgentToolAccessible(agent, tool.name),
	);
}

function availableToolNamesForAgent(agent: AgentEntry): string[] {
	return availableToolNames.value.filter((name) =>
		isAgentToolAccessible(agent, name),
	);
}

function availableToolNamesForResolution(): string[] {
	return availableTools.value.map((tool) => tool.name);
}

function agentToolResolution(agent: AgentEntry): ResolvedToolPermissions {
	return resolveToolPermissions({
		preset: agentPermissionsPreset(agent),
		availableTools: availableToolNamesForResolution(),
		agentTools: agent.permissions?.tools,
		agentDisabledTools: agent.permissions?.disabled_tools,
	});
}

function channelToolResolution(
	agent: AgentEntry,
	channel: AgentChannel,
): ResolvedToolPermissions {
	return resolveToolPermissions({
		preset: agentPermissionsPreset(agent),
		availableTools: availableToolNamesForResolution(),
		agentTools: agent.permissions?.tools,
		agentDisabledTools: agent.permissions?.disabled_tools,
		overrideDisabledTools: channel.disabled_tools,
	});
}

function entryToolResolution(
	agent: AgentEntry,
	channel: AgentChannel,
	entry: AllowFromEntry,
): ResolvedToolPermissions {
	return resolveToolPermissions({
		preset: agentPermissionsPreset(agent),
		availableTools: availableToolNamesForResolution(),
		agentTools: agent.permissions?.tools,
		agentDisabledTools: agent.permissions?.disabled_tools,
		overrideRestrictTools: entry.restrict_tools,
		overrideDisabledTools: channel.disabled_tools,
	});
}

function agentInspectionTitle(agent: AgentEntry, agentIndex: number): string {
	return `Agent: ${agent.name || `Agent ${agentIndex + 1}`}`;
}

function channelInspectionTitle(
	agent: AgentEntry,
	agentIndex: number,
	channel: AgentChannel,
	channelIndex: number,
): string {
	return `${agentInspectionTitle(agent, agentIndex)} / ${channel.type} ${channelIndex + 1}`;
}

function entryInspectionTitle(
	agent: AgentEntry,
	agentIndex: number,
	channel: AgentChannel,
	channelIndex: number,
	entry: AllowFromEntry,
	entryIndex: number,
): string {
	return `${channelInspectionTitle(agent, agentIndex, channel, channelIndex)} / ${entry.from || `Entry ${entryIndex + 1}`}`;
}

function openToolInspectionModal(
	title: string,
	resolution: ResolvedToolPermissions,
) {
	toolInspectionModal.value = { title, resolution };
}

function closeToolInspectionModal() {
	toolInspectionModal.value = null;
}

const toolInspectionOutput = computed(() =>
	toolInspectionModal.value
		? JSON.stringify(toolInspectionModal.value.resolution, null, 2)
		: "",
);
watch(activeTab, (tab) => {
	if (tab === "agents") {
		void preloadAgentFiles();
	}
	if (tab === "skills" && !installedSkills.value.length) {
		void loadInstalledSkills();
	}
	if (tab === "sessions" && sessionAgent.value) {
		void loadSessions();
	}
});

const PROTECTED_AGENT_FILES = [
	"AGENTS.md",
	"SYSTEM.md",
	"MEMORY.md",
	"RULES.md",
] as const;
const protectedAgentFiles = new Set(
	PROTECTED_AGENT_FILES.map((file) => file.toUpperCase()),
);

interface AgentFileEditorState {
	files: string[];
	selectedFile: string;
	content: string;
	draftFileName: string;
	loaded: boolean;
	loading: boolean;
	saving: boolean;
	deleting: boolean;
	creating: boolean;
	syncing: boolean;
	autoSynced: boolean;
	saveFlash: boolean;
	error: string;
}
const agentFileEditorState = ref<Record<string, AgentFileEditorState>>({});
const agentFileSaveTimers: Record<string, ReturnType<typeof setTimeout>> = {};

function getAgentFileState(agentName: string): AgentFileEditorState {
	if (!agentFileEditorState.value[agentName]) {
		agentFileEditorState.value[agentName] = {
			files: [],
			selectedFile: "",
			content: "",
			draftFileName: "",
			loaded: false,
			loading: false,
			saving: false,
			deleting: false,
			creating: false,
			syncing: false,
			autoSynced: false,
			saveFlash: false,
			error: "",
		};
	}
	return agentFileEditorState.value[agentName];
}

function flashAgentFileSave(agentName: string) {
	const state = getAgentFileState(agentName);
	state.saveFlash = true;
	if (agentFileSaveTimers[agentName])
		clearTimeout(agentFileSaveTimers[agentName]);
	agentFileSaveTimers[agentName] = setTimeout(() => {
		state.saveFlash = false;
		delete agentFileSaveTimers[agentName];
	}, 3200);
}

function isProtectedAgentFile(file: string): boolean {
	return protectedAgentFiles.has(file.toUpperCase());
}

function canDeleteAgentFile(file: string): boolean {
	return file !== "" && !isProtectedAgentFile(file);
}

function normalizeNewAgentFileName(file: string): string {
	const trimmed = file.trim();
	if (!trimmed) return "";
	return trimmed.toLowerCase().endsWith(".md") ? trimmed : `${trimmed}.md`;
}

async function readAgentFile(agentName: string, file: string) {
	const state = getAgentFileState(agentName);
	state.error = "";
	state.content = await callTool("agent_file_read", {
		agent: agentName,
		file,
	});
	state.selectedFile = file;
}

async function loadAgentFiles(agentName: string) {
	if (!agentName) return;
	const state = getAgentFileState(agentName);
	state.loading = true;
	state.error = "";
	try {
		let raw = await callTool("agent_file_list", { agent: agentName });
		const allFiles = safeJsonParse<string[]>(raw, []);
		state.files = allFiles.filter((f) => !f.includes("/"));
		if (state.files.length === 0 && !state.autoSynced) {
			state.autoSynced = true;
			await callTool("agent_template_sync", { agent: agentName });
			raw = await callTool("agent_file_list", { agent: agentName });
			state.files = safeJsonParse<string[]>(raw, []).filter(
				(f) => !f.includes("/"),
			);
		}
		if (state.selectedFile && state.files.includes(state.selectedFile)) {
			await readAgentFile(agentName, state.selectedFile);
		} else if (state.files.length > 0) {
			await readAgentFile(agentName, state.files[0]);
		} else {
			state.selectedFile = "";
			state.content = "";
		}
		state.loaded = true;
	} catch (e) {
		state.error = e instanceof Error ? e.message : String(e);
	} finally {
		state.loading = false;
	}
}

function moveAgentFileState(previousName: string, nextName: string) {
	if (!previousName || !nextName || previousName === nextName) return;
	const previousState = agentFileEditorState.value[previousName];
	if (!previousState || agentFileEditorState.value[nextName]) return;
	agentFileEditorState.value[nextName] = {
		...previousState,
		loaded: false,
	};
	delete agentFileEditorState.value[previousName];
}

async function ensureSelectedAgentFilesLoaded() {
	if (activeTab.value !== "agents" || selectedAgentSubtab.value !== "general") {
		return;
	}
	const agentName =
		draft.value.agents[selectedAgentIdx.value]?.name?.trim() ?? "";
	if (!agentName) return;
	const state = getAgentFileState(agentName);
	if (state.loading || state.loaded) return;
	await loadAgentFiles(agentName);
}

watch(
	() => draft.value.agents.map((agent) => agent.name),
	(nextNames, previousNames = []) => {
		for (let i = 0; i < nextNames.length; i += 1) {
			const nextName = nextNames[i]?.trim() ?? "";
			const previousName = previousNames[i]?.trim() ?? "";
			if (nextName && previousName && nextName !== previousName) {
				moveAgentFileState(previousName, nextName);
			}
		}
	},
);

watch(
	[
		activeTab,
		selectedAgentSubtab,
		selectedAgentIdx,
		() => draft.value.agents[selectedAgentIdx.value]?.name,
	],
	() => {
		void ensureSelectedAgentFilesLoaded();
	},
);

async function selectAgentFile(agentName: string, file: string) {
	if (!agentName) return;
	const state = getAgentFileState(agentName);
	state.loading = true;
	state.error = "";
	try {
		await readAgentFile(agentName, file);
	} catch (e) {
		state.error = e instanceof Error ? e.message : String(e);
	} finally {
		state.loading = false;
	}
}

async function saveAgentFile(agentName: string) {
	if (!agentName) return;
	const state = getAgentFileState(agentName);
	if (!state.selectedFile) return;
	state.saving = true;
	state.error = "";
	try {
		await callTool("agent_file_write", {
			agent: agentName,
			file: state.selectedFile,
			content: state.content,
		});
		flashAgentFileSave(agentName);
	} catch (e) {
		state.error = e instanceof Error ? e.message : String(e);
	} finally {
		state.saving = false;
	}
}

async function syncAgentTemplates(agentName: string) {
	if (!agentName) return;
	const state = getAgentFileState(agentName);
	state.syncing = true;
	state.error = "";
	try {
		state.autoSynced = true;
		await callTool("agent_template_sync", { agent: agentName });
		await loadAgentFiles(agentName);
		flashHeaderNotice(`Templates synced for ${agentName}`);
	} catch (e) {
		state.error = e instanceof Error ? e.message : String(e);
	} finally {
		state.syncing = false;
	}
}

async function createAgentFile(agentName: string) {
	if (!agentName) return;
	const state = getAgentFileState(agentName);
	const file = normalizeNewAgentFileName(state.draftFileName);
	if (!file) {
		state.error = "file name is required";
		return;
	}
	state.creating = true;
	state.error = "";
	try {
		await callTool("agent_file_write", {
			agent: agentName,
			file,
			content: "",
		});
		state.draftFileName = "";
		await loadAgentFiles(agentName);
		state.selectedFile = file;
	} catch (e) {
		state.error = e instanceof Error ? e.message : String(e);
	} finally {
		state.creating = false;
	}
}

async function deleteAgentFile(agentName: string) {
	if (!agentName) return;
	const state = getAgentFileState(agentName);
	if (!canDeleteAgentFile(state.selectedFile)) return;
	state.deleting = true;
	state.error = "";
	try {
		const deletedFile = state.selectedFile;
		await callTool("agent_file_delete", {
			agent: agentName,
			file: deletedFile,
		});
		await loadAgentFiles(agentName);
		if (
			state.selectedFile === deletedFile &&
			!state.files.includes(deletedFile)
		) {
			state.selectedFile = state.files[0] ?? "";
		}
	} catch (e) {
		state.error = e instanceof Error ? e.message : String(e);
	} finally {
		state.deleting = false;
	}
}

async function preloadAgentFiles() {
	await Promise.all(
		(draft.value.agents ?? [])
			.map((agent) => agent.name.trim())
			.filter(Boolean)
			.map((agentName) => loadAgentFiles(agentName)),
	);
}

onMounted(async () => {
	connectWs();
	window.addEventListener("keydown", onWindowKeydown);
	await loadConfig();
	await refreshCredentials();
	if (
		activeTab.value === "sessions" &&
		!sessionAgent.value &&
		draft.value.agents.length > 0
	) {
		sessionAgent.value = draft.value.agents[0].name;
	}
});

onUnmounted(() => {
	settingsWs?.close();
	settingsWs = null;
	window.removeEventListener("keydown", onWindowKeydown);
	if (saveSuccessTimer) {
		clearTimeout(saveSuccessTimer);
		saveSuccessTimer = null;
	}
});

function onWindowKeydown(event: KeyboardEvent) {
	if (event.key === "Escape") {
		if (toolInspectionModal.value) {
			closeToolInspectionModal();
		}
		return;
	}
	if (!(event.metaKey || event.ctrlKey) || event.key.toLowerCase() !== "s") {
		return;
	}
	event.preventDefault();
	if (
		loading.value ||
		saving.value ||
		reverting.value ||
		!hasDraftChanges.value
	) {
		return;
	}
	void saveAll();
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
		scheduler: { concurrency: "", precompute_tasks: true },
		skills: {},
	};
}

function hydrateDraftConfig(config: AppConfig): AppConfig {
	const hydrated = JSON.parse(JSON.stringify(config)) as AppConfig;
	if (hydrated.scheduler.precompute_tasks === undefined) {
		hydrated.scheduler.precompute_tasks = true;
	}
	if (hydrated.browser.reuse_tabs === undefined) {
		hydrated.browser.reuse_tabs = true;
	}
	hydrated.agents.forEach((agent) => {
		sanitizeAgentToolSelections(agent);
		(agent.channels ?? []).forEach((ch) => {
			if (ch.enabled === undefined) ch.enabled = true;
			if (ch.show_typing === undefined) ch.show_typing = true;
			if (ch.reply_to_replies === undefined) ch.reply_to_replies = true;
			if (ch.react_to_emoji === undefined) ch.react_to_emoji = true;
			if (ch.send_read_receipts === undefined) ch.send_read_receipts = true;
			(ch.allow_from ?? []).forEach((entry) => {
				if (entry.enabled === undefined) entry.enabled = true;
				if (entry.respond_to_mentions === undefined) {
					entry.respond_to_mentions = true;
				}
			});
		});
		(agent.tasks ?? []).forEach((task) => {
			if (task.enabled === undefined) task.enabled = true;
			if (!task.target) task.target = "";
		});
	});
	return hydrated;
}

function digitsOnly(value: string): string {
	return value.replace(/\D+/g, "");
}

function normalizePortValue(value: string): number {
	if (!value) return 0;
	const parsed = Number.parseInt(value, 10);
	if (Number.isNaN(parsed) || parsed < 1 || parsed > 65535) return 0;
	return parsed;
}

function updateServerPortInput(event: Event) {
	const input = event.target as HTMLInputElement;
	const nextValue = digitsOnly(input.value);
	serverPortInput.value = nextValue;
	draft.value.server.port = normalizePortValue(nextValue);
	if (input.value !== nextValue) {
		input.value = nextValue;
	}
}

function updateCDPPortInput(event: Event) {
	const input = event.target as HTMLInputElement;
	const nextValue = digitsOnly(input.value);
	cdpPortInput.value = nextValue;
	draft.value.browser.cdp_port = normalizePortValue(nextValue);
	if (input.value !== nextValue) {
		input.value = nextValue;
	}
}

async function loadConfig() {
	loading.value = true;
	errorMessage.value = "";
	okMessage.value = "";
	try {
		await store.fetchConfig();
		const cfg = hydrateDraftConfig(store.config ?? emptyConfig());
		draft.value = cfg;
		slackWorkspaceBrowsers.value = {};
		serverPortInput.value = cfg.server.port > 0 ? String(cfg.server.port) : "";
		cdpPortInput.value =
			cfg.browser.cdp_port > 0 ? String(cfg.browser.cdp_port) : "";
		concurrencyInput.value = cfg.scheduler.concurrency
			? String(cfg.scheduler.concurrency)
			: "";

		if (!draft.value.agents.length) {
			await importAgents();
		}

		// Re-resolve agent index now that agents are loaded from config.
		if (route.params.agent) {
			selectedAgentIdx.value = agentIdxFromParam(route.params.agent as string);
		}

		if (!sessionAgent.value && draft.value.agents.length) {
			sessionAgent.value = draft.value.agents[0].name;
		}
		if (activeTab.value === "agents") {
			await preloadAgentFiles();
		}
		// Fetch the available tool list once so the permissions UI can render.
		if (!availableTools.value.length) {
			availableTools.value = await listTools().catch(() => []);
		}
		await loadInstalledSkills();
		lastSavedSnapshot = normalizedDraftSnapshot();
	} catch (e) {
		errorMessage.value = e instanceof Error ? e.message : String(e);
	} finally {
		loading.value = false;
	}
}

async function loadInstalledSkills() {
	skillsLoading.value = true;
	try {
		const raw = await callTool("skills_list");
		installedSkills.value = safeJsonParse<InstalledSkill[]>(raw, []);
	} catch {
		installedSkills.value = [];
	} finally {
		skillsLoading.value = false;
	}
}

function skillConfig(name: string): SkillConfig {
	if (!draft.value.skills[name]) {
		draft.value.skills[name] = { settings: {} };
	}
	if (!draft.value.skills[name].settings) {
		draft.value.skills[name].settings = {};
	}
	return draft.value.skills[name];
}

function skillSettingEntries(
	skill: InstalledSkill,
): Array<[string, SkillSettingSchema]> {
	return Object.entries(skill.settings_schema?.properties ?? {});
}

function skillSettingInputKind(schema: SkillSettingSchema): "string" | "array" {
	return schema.type === "array" ? "array" : "string";
}

function skillSettingLabel(key: string, schema: SkillSettingSchema): string {
	return schema.title || key.replaceAll("_", " ");
}

function skillSettingPlaceholder(schema: SkillSettingSchema): string {
	return schema.placeholder || "";
}

function skillStringSetting(name: string, key: string): string {
	const value = skillConfig(name).settings?.[key];
	return typeof value === "string" ? value : "";
}

function setSkillStringSetting(name: string, key: string, event: Event) {
	const value = (event.target as HTMLInputElement).value.trim();
	const settings = { ...(skillConfig(name).settings ?? {}) };
	if (value) {
		settings[key] = value;
	} else {
		delete settings[key];
	}
	skillConfig(name).settings = settings;
}

function skillArraySetting(name: string, key: string): string {
	const value = skillConfig(name).settings?.[key];
	return Array.isArray(value)
		? value
				.filter((item): item is string => typeof item === "string")
				.join(", ")
		: "";
}

function setSkillArraySetting(name: string, key: string, event: Event) {
	const values = (event.target as HTMLInputElement).value
		.split(",")
		.map((value) => value.trim())
		.filter(Boolean);
	const settings = { ...(skillConfig(name).settings ?? {}) };
	if (values.length) {
		settings[key] = values;
	} else {
		delete settings[key];
	}
	skillConfig(name).settings = settings;
}

function addAgent() {
	const agent: AgentEntry = {
		name: "",
		model: "",
		memory: "",
		rules: "",
		fallbacks: [],
		channels: [],
		tasks: [],
	};
	draft.value.agents.push(agent);
	// Setting the index triggers the watch which navigates to _N/general.
	selectedAgentIdx.value = draft.value.agents.length - 1;
}

const removeAgentTarget = ref<number | null>(null);
const removeAgentOpen = ref(false);
const removeAgentBtns = ref<HTMLElement>();
watch(removeAgentOpen, (v) => {
	if (v)
		setTimeout(() => {
			const btns =
				removeAgentBtns.value?.querySelectorAll<HTMLElement>("button");
			btns?.[btns.length - 1]?.focus();
		});
});

function removeAgent(index: number) {
	removeAgentTarget.value = index;
	removeAgentOpen.value = true;
}

function confirmRemoveAgent() {
	const index = removeAgentTarget.value;
	if (index === null) return;
	removeAgentTarget.value = null;
	removeAgentOpen.value = false;
	draft.value.agents.splice(index, 1);
	const next = Math.min(
		selectedAgentIdx.value,
		Math.max(0, draft.value.agents.length - 1),
	);
	if (next !== selectedAgentIdx.value) {
		selectedAgentIdx.value = next;
	} else {
		// Index unchanged but the agent at this slot may have changed; update URL.
		void router.replace(agentRoutePath(next, selectedAgentSubtab.value));
	}
}

const deleteFileTarget = ref<{ agentName: string; file: string } | null>(null);
const deleteFileBtns = ref<HTMLElement>();
watch(deleteFileTarget, (v) => {
	if (v)
		setTimeout(() => {
			const btns =
				deleteFileBtns.value?.querySelectorAll<HTMLElement>("button");
			btns?.[btns.length - 1]?.focus();
		});
});

function promptDeleteAgentFile(agentName: string) {
	const state = getAgentFileState(agentName);
	if (!canDeleteAgentFile(state.selectedFile)) return;
	deleteFileTarget.value = { agentName, file: state.selectedFile };
}

async function confirmDeleteAgentFile() {
	const target = deleteFileTarget.value;
	if (!target) return;
	deleteFileTarget.value = null;
	await deleteAgentFile(target.agentName);
}

function onAgentNameChange(agentEntry: AgentEntry) {
	if (agentEntry.name) {
		void loadAgentFiles(agentEntry.name);
	}
}

function onTaskNameFocus() {
	editingTaskOriginalName.value = selectedTask?.value?.name ?? null;
	renameTaskError.value = null;
}

async function onTaskNameBlur() {
	if (!editingTaskOriginalName.value) return;
	const oldName = editingTaskOriginalName.value;
	editingTaskOriginalName.value = null;
	const newName = selectedTask?.value?.name ?? "";
	if (newName === oldName) return;
	renamingTask.value = true;
	try {
		const agentName = draft.value.agents[selectedAgentIdx.value]?.name ?? "";
		if (!agentName) throw new Error("agent name required to rename task");
		await callTool("config_task_rename", {
			agent: agentName,
			task: oldName,
			new_name: newName,
		});
		await loadConfig();
	} catch (e) {
		renameTaskError.value = e instanceof Error ? e.message : String(e);
		// revert on error
		if (selectedTask?.value) selectedTask.value.name = oldName;
	} finally {
		renamingTask.value = false;
	}
}

function addTask(agentIndex: number) {
	const task: AgentTask = {
		enabled: true,
		name: `scheduled-${Date.now()}`,
		type: "prompt",
		prompt: "",
		schedule: "",
		watch: "",
		target: "",
		run_once: false,
		from_file: true,
	};
	if (!Array.isArray(draft.value.agents[agentIndex].tasks)) {
		draft.value.agents[agentIndex].tasks = [];
	}
	draft.value.agents[agentIndex].tasks.push(task);
	// Focus the new task's name field so the user can rename it immediately.
	void nextTick().then(() => {
		try {
			// Use requestAnimationFrame twice to ensure the element is painted
			// and inserted in the DOM before attempting to focus it.
			requestAnimationFrame(() => {
				requestAnimationFrame(() => {
					const el = document.querySelector<HTMLInputElement>(
						`input[data-task-id="${task.name}"]`,
					);
					el?.focus();
					el?.select();
				});
			});
		} catch {
			// ignore
		}
	});
}

function removeTask(agentIndex: number, taskIndex: number) {
	draft.value.agents[agentIndex].tasks.splice(taskIndex, 1);
}

async function convertTaskToScript(agentName: string, taskName: string) {
	errorMessage.value = "";
	okMessage.value = "";
	try {
		await callTool("config_task_convert_to_script", {
			agent: agentName,
			task: taskName,
		});
		compileToastVisible.value = true;
	} catch (e) {
		errorMessage.value = e instanceof Error ? e.message : String(e);
	}
}

async function moveTaskToFile(
	agentIndex: number,
	taskIndex: number,
	agentName: string,
	taskName: string,
) {
	errorMessage.value = "";
	okMessage.value = "";
	try {
		const result = await callTool("config_task_move_to_file", {
			agent: agentName,
			task: taskName,
		});
		// Keep the task visible in the UI but mark it as coming from a file so
		// it renders as `tasks/<name>.md` instead of disappearing until reload.
		const taskObj = draft.value?.agents?.[agentIndex]?.tasks?.[taskIndex];
		if (taskObj) {
			// Try to parse a returned path (e.g. "tasks/foo.md") from the tool result.
			let parsed: string | null = null;
			if (typeof result === "string") {
				const m = result.match(/tasks\/[^\s]+\.md/);
				if (m?.[0]) parsed = m[0];
			}
			if (parsed) {
				taskObj.file = parsed;
			} else {
				// Fallback: sanitize the taskName or task.name and guess a filename.
				const src = (taskName || taskObj.name || "task").toLowerCase();
				const base = src
					.replace(/[^a-z0-9-_]+/g, "-")
					.replace(/-+/g, "-")
					.replace(/(^-|-$)/g, "");
				taskObj.file = `tasks/${base || "task"}.md`;
			}
			taskObj.from_file = true;
		}
		okMessage.value = result;
	} catch (e) {
		errorMessage.value = e instanceof Error ? e.message : String(e);
	}
}

function configuredChannelLabel(ch: AgentChannel, index: number): string {
	if (ch.id) return `${ch.type} via ${ch.id}`;
	return `${ch.type} via #${index + 1}`;
}

function channelPrimaryLabel(ch: AgentChannel): string {
	switch (ch.type) {
		case "slack":
			return "Primary Slack Sender ID (optional)";
		case "signal":
			return "Primary Signal Sender ID (optional)";
		case "discord":
			return "Primary Discord Sender ID (optional)";
		default:
			return "Primary Sender ID (optional)";
	}
}

function channelPrimaryPlaceholder(ch: AgentChannel): string {
	switch (ch.type) {
		case "slack":
			return "e.g. U0123456789";
		case "signal":
			return "e.g. +15551234567";
		case "discord":
			return "e.g. 123456789012345678";
		default:
			return "Enter a user ID";
	}
}

function channelPrimaryHelp(ch: AgentChannel): string {
	switch (ch.type) {
		case "slack":
			return "Messages from this Slack sender ID will be marked as coming from the primary person in history/context.";
		case "signal":
			return "Messages from this Signal sender ID will be marked as coming from the primary person in history/context.";
		case "discord":
			return "Messages from this Discord sender ID will be marked as coming from the primary person in history/context.";
		default:
			return "Messages from this sender ID will be marked as coming from the primary person in history/context.";
	}
}

function selectedTaskTarget(task: AgentTask): string {
	return parseTaskChannelValue(task.target).target;
}

function setSelectedTaskTarget(task: AgentTask, value: string) {
	const parsed = parseTaskChannelValue(task.target);
	const trimmed = value.trim();
	if (!parsed.selection) {
		task.target = "";
		return;
	}
	if (parsed.type === "session") {
		task.target = `session:${trimmed}`;
		return;
	}
	task.target = `${parsed.selection}:${trimmed}`;
}

function selectedConfiguredChannel(
	agent: AgentEntry,
	task: AgentTask,
): AgentChannel | null {
	const selection = taskChannelSelection(task);
	if (!selection || selection === "session") return null;
	const [type, configuredID] = selection.split(":", 2);
	return (
		(agent.channels ?? []).find(
			(ch) => (ch.type ?? "") === type && (ch.id ?? "") === configuredID,
		) ?? null
	);
}

function selectedConfiguredChannelIndex(
	agent: AgentEntry,
	task: AgentTask,
): number {
	const selection = taskChannelSelection(task);
	if (!selection || selection === "session") return -1;
	const [type, configuredID] = selection.split(":", 2);
	return (agent.channels ?? []).findIndex(
		(ch) => (ch.type ?? "") === type && (ch.id ?? "") === configuredID,
	);
}

function taskDeliveryTargetLabel(agent: AgentEntry, task: AgentTask): string {
	const parsed = parseTaskChannelValue(task.target);
	if (parsed.type === "session") return "Session name";
	const channel = selectedConfiguredChannel(agent, task);
	switch (channel?.type) {
		case "slack":
			return "Slack channel ID";
		case "discord":
			return "Discord channel ID";
		case "signal":
			return "Signal recipient or group ID";
		default:
			return "Delivery ID";
	}
}

function slackChannelsForTask(
	agentIndex: number,
	agent: AgentEntry,
	task: AgentTask,
): SlackWorkspaceChannelOption[] {
	const channelIndex = selectedConfiguredChannelIndex(agent, task);
	if (channelIndex < 0) return [];
	return slackVisibleChannels(agentIndex, channelIndex);
}

function slackVisibleChannels(
	agentIndex: number,
	channelIndex: number,
): SlackWorkspaceChannelOption[] {
	return (
		slackWorkspaceState(agentIndex, channelIndex).result?.channels ?? []
	).filter((channel) => !channel.is_archived);
}

function slackTargetPlaceholder(agent: AgentEntry, task: AgentTask): string {
	const channel = selectedConfiguredChannel(agent, task);
	if (channel?.type === "slack") return "C1234567890";
	if (channel?.type === "discord") return "123456789012345678";
	if (channel?.type === "signal") return "+15551234567 or group ID";
	return "Target ID";
}

function configuredTaskChannelOptions(agent: AgentEntry): TaskChannelOption[] {
	return [
		{ value: "session", label: "session", type: "session" },
		...(agent.channels ?? [])
			.filter((ch) => !!ch.type && !!ch.id)
			.map((ch, index) => ({
				value: `${ch.type}:${ch.id}`,
				label: configuredChannelLabel(ch, index),
				type: ch.type,
			})),
	];
}

function parseTaskChannelValue(target?: string): {
	selection: string;
	target: string;
	type: string;
} {
	const raw = (target ?? "").trim();
	if (!raw) return { selection: "", target: "", type: "" };
	if (raw.startsWith("session:")) {
		return {
			selection: "session",
			target: raw.slice("session:".length),
			type: "session",
		};
	}
	const parts = raw.split(":", 3);
	if (parts.length === 3) {
		return {
			selection: `${parts[0]}:${parts[1]}`,
			target: parts[2] ?? "",
			type: parts[0] ?? "",
		};
	}
	return { selection: "", target: "", type: "" };
}

function taskChannelSelection(task: AgentTask): string {
	return parseTaskChannelValue(task.target).selection;
}

function isChannelEnabled(channel: AgentChannel): boolean {
	return channel.enabled !== false;
}

function isAllowFromEnabled(entry: AllowFromEntry): boolean {
	return entry.enabled !== false;
}

function isTaskEnabled(task: AgentTask): boolean {
	return task.enabled !== false;
}

// Toggle the currently-selected task's enabled state. Using selectedAgentIdx/selectedTaskIdx
// ensures we mutate the task object directly on the reactive draft structure.

function setSelectedTaskEnabled(val: boolean) {
	const agent = draft.value.agents[selectedAgentIdx.value];
	const idx = selectedTaskIdx.value ?? -1;
	if (!agent || idx < 0 || idx >= (agent.tasks?.length ?? 0)) return;
	const task = agent.tasks[idx];
	task.enabled = val;
}

function setChannelEnabled(channel: AgentChannel, val: boolean) {
	channel.enabled = val;
}

function setAllowFromEnabled(entry: AllowFromEntry, val: boolean) {
	entry.enabled = val;
}

function taskDefinedIn(task: AgentTask | null): string {
	if (!task) return "";
	if (task.file && String(task.file).trim()) return String(task.file);
	// If task is marked as coming from a file but no explicit file path is present,
	// try to infer from the task name; otherwise it's in aviary.yaml
	if (task.from_file) {
		const name = (task.name || "").trim();
		if (name) return `tasks/${name.replace(/[^a-z0-9-_]+/gi, "-")}.md`;
		return "tasks/*.md";
	}
	return "aviary.yaml";
}

function statusBadgeClass(enabled: boolean): string {
	return enabled
		? "rounded-full bg-emerald-100 px-2 py-1 text-[11px] font-semibold uppercase tracking-wide text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300"
		: "rounded-full bg-gray-200 px-2 py-1 text-[11px] font-semibold uppercase tracking-wide text-gray-600 dark:bg-gray-800 dark:text-gray-300";
}

function channelCardClass(channel: AgentChannel): string {
	return isChannelEnabled(channel)
		? "border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-900"
		: "border-gray-300 bg-gray-50 opacity-75 dark:border-gray-700 dark:bg-gray-950";
}

function allowFromCardClass(entry: AllowFromEntry): string {
	return isAllowFromEnabled(entry)
		? "border-gray-100 bg-white dark:border-gray-800 dark:bg-gray-900"
		: "border-gray-200 bg-gray-50 opacity-75 dark:border-gray-800 dark:bg-gray-950";
}

function setTaskChannelSelection(task: AgentTask, event: Event) {
	const selection = (event.target as HTMLSelectElement).value;
	const parsed = parseTaskChannelValue(task.target);
	if (!selection) {
		task.target = "";
		return;
	}
	if (selection === "session") {
		task.target = `session:${parsed.type === "session" ? parsed.target : ""}`;
		return;
	}
	task.target = `${selection}:${parsed.type === "session" ? "" : parsed.target}`;
}

function addChannel(agentIndex: number) {
	const ch: AgentChannel = {
		enabled: true,
		type: "signal",
		show_typing: true,
		reply_to_replies: true,
		react_to_emoji: true,
		send_read_receipts: true,
	};
	if (!Array.isArray(draft.value.agents[agentIndex].channels)) {
		draft.value.agents[agentIndex].channels = [];
	}
	draft.value.agents[agentIndex].channels.push(ch);
}

function removeChannel(agentIndex: number, chIndex: number) {
	draft.value.agents[agentIndex].channels.splice(chIndex, 1);
}

async function browseSlackChannels(
	agentIndex: number,
	channelIndex: number,
	channel: AgentChannel,
) {
	const state = slackWorkspaceState(agentIndex, channelIndex);
	state.loading = true;
	state.error = "";
	try {
		const raw = await callTool("slack_channels_list", {
			bot_token: channel.token ?? "",
		});
		state.result = safeJsonParse<SlackWorkspaceBrowseResult>(raw, {
			channels: [],
		});
		if (!Array.isArray(state.result.channels)) {
			state.result.channels = [];
		}
	} catch (error) {
		state.result = null;
		state.error = error instanceof Error ? error.message : String(error);
	} finally {
		state.loading = false;
	}
}

function addAllowFrom(agentIndex: number, chIndex: number) {
	const ch = draft.value.agents[agentIndex].channels[chIndex];
	if (!Array.isArray(ch.allow_from)) {
		ch.allow_from = [];
	}
	ch.allow_from.push({ enabled: true, from: "", respond_to_mentions: true });
}

function removeAllowFrom(
	agentIndex: number,
	chIndex: number,
	entryIndex: number,
) {
	draft.value.agents[agentIndex].channels[chIndex].allow_from?.splice(
		entryIndex,
		1,
	);
}

function entryMentionPrefixes(entry: AllowFromEntry): string {
	return (entry.mention_prefixes ?? []).join(", ");
}

function setEntryMentionPrefixes(entry: AllowFromEntry, event: Event) {
	entry.mention_prefixes = (event.target as HTMLInputElement).value
		.split(",")
		.map((v) => v.trim())
		.filter(Boolean);
}

function entryExcludePrefixes(entry: AllowFromEntry): string {
	return (entry.exclude_prefixes ?? []).join(", ");
}

function setEntryExcludePrefixes(entry: AllowFromEntry, event: Event) {
	entry.exclude_prefixes = (event.target as HTMLInputElement).value
		.split(",")
		.map((v) => v.trim())
		.filter(Boolean);
}

function hasEntryToolRestriction(entry: AllowFromEntry): boolean {
	return (entry.restrict_tools?.length ?? 0) > 0;
}

function setEntryToolRestriction(entry: AllowFromEntry, restricted: boolean) {
	if (restricted) {
		const agent = draft.value.agents.find((candidate) =>
			candidate.channels?.some((channel) =>
				channel.allow_from?.includes(entry),
			),
		);
		entry.restrict_tools = agent
			? availableToolsForAgent(agent).map((t) => t.name)
			: availableTools.value.map((t) => t.name);
	} else {
		entry.restrict_tools = undefined;
	}
}

function isEntryToolEnabled(entry: AllowFromEntry, toolName: string): boolean {
	if (!hasEntryToolRestriction(entry)) return true;
	const agent = draft.value.agents.find((candidate) =>
		candidate.channels?.some((channel) => channel.allow_from?.includes(entry)),
	);
	if (agent && !isAgentToolAccessible(agent, toolName)) return false;
	return entry.restrict_tools?.includes(toolName) ?? false;
}

function toggleEntryTool(
	entry: AllowFromEntry,
	toolName: string,
	enabled: boolean,
) {
	const agent = draft.value.agents.find((candidate) =>
		candidate.channels?.some((channel) => channel.allow_from?.includes(entry)),
	);
	if (agent && !isAgentToolAccessible(agent, toolName)) return;
	if (!entry.restrict_tools) entry.restrict_tools = [];
	const idx = entry.restrict_tools.indexOf(toolName);
	if (enabled && idx === -1) {
		entry.restrict_tools.push(toolName);
	} else if (!enabled && idx !== -1) {
		entry.restrict_tools.splice(idx, 1);
	}
}

function isEntryCategoryFullyEnabled(
	entry: AllowFromEntry,
	cat: string,
): boolean {
	const catTools = availableTools.value.filter(
		(t) => toolCategory(t.name) === cat,
	);
	const agent = draft.value.agents.find((candidate) =>
		candidate.channels?.some((channel) => channel.allow_from?.includes(entry)),
	);
	const accessibleTools = agent
		? catTools.filter((tool) => isAgentToolAccessible(agent, tool.name))
		: catTools;
	if (!accessibleTools.length) return false;
	return accessibleTools.every((t) => isEntryToolEnabled(entry, t.name));
}

function isEntryCategoryPartiallyEnabled(
	entry: AllowFromEntry,
	cat: string,
): boolean {
	const catTools = availableTools.value.filter(
		(t) => toolCategory(t.name) === cat,
	);
	const agent = draft.value.agents.find((candidate) =>
		candidate.channels?.some((channel) => channel.allow_from?.includes(entry)),
	);
	const accessibleTools = agent
		? catTools.filter((tool) => isAgentToolAccessible(agent, tool.name))
		: catTools;
	const enabledCount = accessibleTools.filter((t) =>
		isEntryToolEnabled(entry, t.name),
	).length;
	return enabledCount > 0 && enabledCount < accessibleTools.length;
}

function toggleEntryCategory(
	entry: AllowFromEntry,
	cat: string,
	enabled: boolean,
) {
	const catTools = availableTools.value.filter(
		(t) => toolCategory(t.name) === cat,
	);
	const agent = draft.value.agents.find((candidate) =>
		candidate.channels?.some((channel) => channel.allow_from?.includes(entry)),
	);
	for (const t of catTools) {
		if (agent && !isAgentToolAccessible(agent, t.name)) continue;
		toggleEntryTool(entry, t.name, enabled);
	}
}

function hasToolRestriction(agent: AgentEntry): boolean {
	return (agent.permissions?.tools?.length ?? 0) > 0;
}

function agentFilesystemAllowedPaths(agent: AgentEntry): string {
	return (agent.permissions?.filesystem?.allowed_paths ?? []).join("\n");
}

function setAgentFilesystemAllowedPaths(agent: AgentEntry, event: Event) {
	const value = (event.target as HTMLTextAreaElement).value;
	const allowed_paths = value
		.split(/\r?\n/)
		.map((v) => v.trim())
		.filter(Boolean);
	agent.permissions = {
		...(agent.permissions ?? {}),
		filesystem: allowed_paths.length ? { allowed_paths } : undefined,
	};
}

function agentExecAllowedCommands(agent: AgentEntry): string {
	return (agent.permissions?.exec?.allowed_commands ?? []).join("\n");
}

function setAgentExecAllowedCommands(agent: AgentEntry, event: Event) {
	const value = (event.target as HTMLTextAreaElement).value;
	const allowed_commands = value
		.split(/\r?\n/)
		.map((v) => v.trim())
		.filter(Boolean);
	const currentExec = agent.permissions?.exec;
	const nextExec =
		allowed_commands.length ||
		currentExec?.shell_interpolate ||
		(currentExec?.shell ?? "").trim()
			? {
					allowed_commands: allowed_commands.length
						? allowed_commands
						: undefined,
					shell_interpolate: currentExec?.shell_interpolate ? true : undefined,
					shell: (currentExec?.shell ?? "").trim() || undefined,
				}
			: undefined;
	agent.permissions = {
		...(agent.permissions ?? {}),
		exec: nextExec,
	};
}

function setAgentExecShellInterpolate(agent: AgentEntry, enabled: boolean) {
	const currentExec = agent.permissions?.exec;
	const nextExec =
		(currentExec?.allowed_commands?.length ?? 0) > 0 ||
		enabled ||
		(currentExec?.shell ?? "").trim()
			? {
					allowed_commands: currentExec?.allowed_commands,
					shell_interpolate: enabled ? true : undefined,
					shell: (currentExec?.shell ?? "").trim() || undefined,
				}
			: undefined;
	agent.permissions = {
		...(agent.permissions ?? {}),
		exec: nextExec,
	};
}

function setAgentExecShell(agent: AgentEntry, event: Event) {
	const shell = (event.target as HTMLInputElement).value.trim();
	const currentExec = agent.permissions?.exec;
	const nextExec =
		(currentExec?.allowed_commands?.length ?? 0) > 0 ||
		currentExec?.shell_interpolate ||
		shell
			? {
					allowed_commands: currentExec?.allowed_commands,
					shell_interpolate: currentExec?.shell_interpolate ? true : undefined,
					shell: shell || undefined,
				}
			: undefined;
	agent.permissions = {
		...(agent.permissions ?? {}),
		exec: nextExec,
	};
}

function setToolRestriction(agent: AgentEntry, restricted: boolean) {
	if (restricted) {
		// Start with all tools selected so nothing breaks immediately.
		agent.permissions = {
			...(agent.permissions ?? {}),
			tools: availableToolsForAgent(agent).map((t) => t.name),
		};
	} else {
		if (!agent.permissions) return;
		agent.permissions = {
			...agent.permissions,
			tools: undefined,
		};
	}
}

function isToolEnabled(agent: AgentEntry, toolName: string): boolean {
	if (!hasToolRestriction(agent)) return true;
	if (!isAgentToolAccessible(agent, toolName)) return false;
	return agent.permissions?.tools?.includes(toolName) ?? false;
}

function toggleTool(agent: AgentEntry, toolName: string, enabled: boolean) {
	if (!isAgentToolAccessible(agent, toolName)) return;
	if (!agent.permissions) agent.permissions = { tools: [] };
	if (!agent.permissions.tools) agent.permissions.tools = [];
	const idx = agent.permissions.tools.indexOf(toolName);
	if (enabled && idx === -1) {
		agent.permissions.tools.push(toolName);
	} else if (!enabled && idx !== -1) {
		agent.permissions.tools.splice(idx, 1);
	}
}

function isCategoryFullyEnabled(agent: AgentEntry, cat: string): boolean {
	const catTools = availableTools.value.filter(
		(t) => toolCategory(t.name) === cat,
	);
	const accessibleTools = catTools.filter((tool) =>
		isAgentToolAccessible(agent, tool.name),
	);
	if (!accessibleTools.length) return false;
	return accessibleTools.every((t) => isToolEnabled(agent, t.name));
}

function isCategoryPartiallyEnabled(agent: AgentEntry, cat: string): boolean {
	const catTools = availableTools.value.filter(
		(t) => toolCategory(t.name) === cat,
	);
	const accessibleTools = catTools.filter((tool) =>
		isAgentToolAccessible(agent, tool.name),
	);
	const enabledCount = accessibleTools.filter((t) =>
		isToolEnabled(agent, t.name),
	).length;
	return enabledCount > 0 && enabledCount < accessibleTools.length;
}

function toggleCategory(agent: AgentEntry, cat: string, enabled: boolean) {
	const catTools = availableTools.value.filter(
		(t) => toolCategory(t.name) === cat,
	);
	for (const t of catTools) {
		if (!isAgentToolAccessible(agent, t.name)) continue;
		toggleTool(agent, t.name, enabled);
	}
}

async function importAgents() {
	try {
		const raw = await callTool("agent_list");
		const agents = safeJsonParse<RuntimeAgent[]>(raw, []);
		if (!agents.length) return;
		draft.value.agents = agents.map((agent) => ({
			name: agent.name ?? "",
			model: agent.model ?? "",
			memory: "",
			fallbacks: agent.fallbacks ?? [],
			channels: [],
			tasks: [],
		}));
		if (!sessionAgent.value && draft.value.agents.length) {
			sessionAgent.value = draft.value.agents[0].name;
		}
	} catch {
		// best-effort import
	}
}

const newTaskNameMap = ref<Record<string, string>>({});

function createTaskFromName(agentIndex: number) {
	const agent = draft.value.agents[agentIndex];
	if (!agent) return;
	const key = agent.name || String(agentIndex);
	const desired = (newTaskNameMap.value[key] || "").trim();
	addTask(agentIndex);
	nextTick(() => {
		const tasks = draft.value.agents[agentIndex].tasks;
		const idx = tasks.length - 1;
		if (desired) tasks[idx].name = desired;
		selectedTaskIdx.value = idx;
		newTaskNameMap.value[key] = "";
	});
}

const deleteTaskTarget = ref<{
	agentIndex: number;
	taskIndex: number;
	name?: string;
} | null>(null);
const deleteTaskOpen = ref(false);

function promptDeleteTask(
	agentIndex: number,
	taskIndex: number,
	name?: string,
) {
	deleteTaskTarget.value = { agentIndex, taskIndex, name };
	deleteTaskOpen.value = true;
}

async function confirmDeleteTaskAction() {
	if (!deleteTaskTarget.value) return;
	const draftSnapshot = JSON.parse(JSON.stringify(draft.value)) as AppConfig;
	const previousSelectedTaskIdx = selectedTaskIdx.value;
	const { agentIndex, taskIndex } = deleteTaskTarget.value;
	removeTask(agentIndex, taskIndex);
	const tasks = draft.value.agents[agentIndex]?.tasks ?? [];
	if (tasks.length) {
		selectedTaskIdx.value = Math.min(
			selectedTaskIdx.value ?? 0,
			tasks.length - 1,
		);
	} else {
		selectedTaskIdx.value = null;
	}
	try {
		await persistDraftConfig();
		deleteTaskOpen.value = false;
		deleteTaskTarget.value = null;
	} catch (e) {
		draft.value = draftSnapshot;
		selectedTaskIdx.value = previousSelectedTaskIdx;
		errorMessage.value = e instanceof Error ? e.message : String(e);
	}
}

function normalizedDraftConfig(): AppConfig {
	const normalized = JSON.parse(JSON.stringify(draft.value)) as AppConfig;

	const conc = concurrencyInput.value.trim();
	if (!conc || conc.toLowerCase() === "auto") {
		normalized.scheduler.concurrency = "";
	} else {
		const n = Number.parseInt(conc, 10);
		normalized.scheduler.concurrency = Number.isNaN(n) || n < 1 ? "" : n;
	}
	normalized.scheduler.precompute_tasks =
		normalized.scheduler.precompute_tasks === false ? false : undefined;
	normalized.browser.reuse_tabs =
		normalized.browser.reuse_tabs === false ? false : undefined;

	normalized.search = {
		web: {
			brave_api_key:
				(normalized.search?.web?.brave_api_key ?? "").trim() || undefined,
		},
	};

	for (const agent of normalized.agents ?? []) {
		sanitizeAgentToolSelections(agent);
	}

	normalized.agents = (normalized.agents ?? []).map((agent) => ({
		...agent,
		name: (agent.name ?? "").trim(),
		model: (agent.model ?? "").trim(),
		memory: (agent.memory ?? "").trim(),
		rules: (agent.rules ?? "").trim() || undefined,
		fallbacks: (agent.fallbacks ?? []).map((v) => v.trim()).filter(Boolean),
		channels: (agent.channels ?? []).map((ch) => ({
			...ch,
			enabled: ch.enabled === false ? false : undefined,
			type: (ch.type ?? "").trim(),
			token: (ch.token ?? "").trim() || undefined,
			id: (ch.id ?? "").trim() || undefined,
			url: (ch.url ?? "").trim() || undefined,
			model: (ch.model ?? "").trim() || undefined,
			fallbacks: (ch.fallbacks ?? []).map((v) => v.trim()).filter(Boolean),
			disabled_tools: (ch.disabled_tools ?? [])
				.map((v) => v.trim())
				.filter(Boolean),
			show_typing: ch.show_typing === false ? false : undefined,
			reply_to_replies: ch.reply_to_replies === false ? false : undefined,
			react_to_emoji: ch.react_to_emoji === false ? false : undefined,
			send_read_receipts: ch.send_read_receipts === false ? false : undefined,
			group_chat_history:
				ch.group_chat_history && ch.group_chat_history !== 0
					? ch.group_chat_history
					: undefined,
			allow_from: (ch.allow_from ?? [])
				.map((entry) => ({
					...entry,
					enabled: entry.enabled === false ? false : undefined,
					from: (entry.from ?? "").trim(),
					allowed_groups: (entry.allowed_groups ?? "").trim() || undefined,
					model: (entry.model ?? "").trim() || undefined,
					fallbacks: (entry.fallbacks ?? [])
						.map((v) => v.trim())
						.filter(Boolean),
					mention_prefixes: (entry.mention_prefixes ?? [])
						.map((v) => v.trim())
						.filter(Boolean),
					exclude_prefixes: (entry.exclude_prefixes ?? [])
						.map((v) => v.trim())
						.filter(Boolean),
					restrict_tools: (entry.restrict_tools ?? [])
						.map((v) => v.trim())
						.filter(Boolean),
					mention_prefix_group_only:
						entry.mention_prefix_group_only === false ? false : undefined,
				}))
				.filter((entry) => entry.from),
		})),
		tasks: (agent.tasks ?? []).map((task) => ({
			...task,
			enabled: task.enabled === false ? false : undefined,
			name: (task.name ?? "").trim(),
			type: task.type === "script" ? "script" : "prompt",
			prompt: task.prompt ?? "",
			schedule: (task.schedule ?? "").trim(),
			watch: (task.watch ?? "").trim(),
			start_at: (task.start_at ?? "").trim(),
			target: (task.target ?? "").trim(),
			run_once: Boolean(task.run_once),
		})),
		permissions:
			(agent.permissions?.tools?.length ?? 0) > 0 ||
			(agent.permissions?.disabled_tools?.length ?? 0) > 0 ||
			(agent.permissions?.filesystem?.allowed_paths?.length ?? 0) > 0 ||
			(agent.permissions?.exec?.allowed_commands?.length ?? 0) > 0 ||
			Boolean(agent.permissions?.exec?.shell_interpolate) ||
			Boolean((agent.permissions?.exec?.shell ?? "").trim()) ||
			agentPermissionsPreset(agent) !== "standard"
				? {
						preset:
							agentPermissionsPreset(agent) === "standard"
								? undefined
								: agentPermissionsPreset(agent),
						tools: (agent.permissions?.tools ?? []).filter(Boolean),
						disabled_tools: (agent.permissions?.disabled_tools ?? [])
							.map((v) => v.trim())
							.filter(Boolean),
						filesystem:
							(agent.permissions?.filesystem?.allowed_paths?.length ?? 0) > 0
								? {
										allowed_paths: (
											agent.permissions?.filesystem?.allowed_paths ?? []
										)
											.map((v) => v.trim())
											.filter(Boolean),
									}
								: undefined,
						exec:
							(agent.permissions?.exec?.allowed_commands?.length ?? 0) > 0 ||
							Boolean(agent.permissions?.exec?.shell_interpolate) ||
							Boolean((agent.permissions?.exec?.shell ?? "").trim())
								? {
										allowed_commands: (
											agent.permissions?.exec?.allowed_commands ?? []
										)
											.map((v) => v.trim())
											.filter(Boolean),
										shell_interpolate:
											agent.permissions?.exec?.shell_interpolate === true
												? true
												: undefined,
										shell:
											(agent.permissions?.exec?.shell ?? "").trim() ||
											undefined,
									}
								: undefined,
					}
				: undefined,
	}));
	const normalizedSkills: Record<string, SkillConfig> = {};
	for (const [name, skill] of Object.entries(normalized.skills ?? {}) as Array<
		[string, SkillConfig]
	>) {
		const nextSkill: SkillConfig = {
			enabled: Boolean(skill?.enabled),
			settings: Object.fromEntries(
				Object.entries(skill?.settings ?? {}).flatMap(([key, value]) => {
					if (typeof value === "string") {
						const trimmed = value.trim();
						return trimmed ? [[key, trimmed]] : [];
					}
					if (Array.isArray(value)) {
						const normalizedValues = value
							.filter((item): item is string => typeof item === "string")
							.map((item) => item.trim())
							.filter(Boolean);
						return normalizedValues.length ? [[key, normalizedValues]] : [];
					}
					return value == null ? [] : [[key, value]];
				}),
			),
		};
		if (nextSkill.enabled || Object.keys(nextSkill.settings ?? {}).length > 0) {
			normalizedSkills[name] = nextSkill;
		}
	}
	normalized.skills = normalizedSkills;
	return normalized;
}

function normalizedDraftSnapshot(): string {
	return JSON.stringify(normalizedDraftConfig());
}

function flashSaveSuccess() {
	flashHeaderNotice("Settings saved");
}

function flashHeaderNotice(message: string) {
	headerNoticeText.value = message;
	saveSuccessVisible.value = true;
	if (saveSuccessTimer) {
		clearTimeout(saveSuccessTimer);
	}
	saveSuccessTimer = setTimeout(() => {
		saveSuccessVisible.value = false;
		saveSuccessTimer = null;
	}, 3200);
}
async function saveAll() {
	saving.value = true;
	errorMessage.value = "";
	okMessage.value = "";
	try {
		await persistDraftConfig();
	} catch (e) {
		errorMessage.value = e instanceof Error ? e.message : String(e);
	} finally {
		saving.value = false;
	}
}

async function persistDraftConfig() {
	const normalized = normalizedDraftConfig();
	const snapshot = JSON.stringify(normalized);
	if (snapshot === lastSavedSnapshot) {
		okMessage.value = "Settings already up to date.";
		return;
	}

	await store.saveConfig(normalized);
	lastSavedSnapshot = snapshot;
	draft.value = hydrateDraftConfig(normalized);
	serverPortInput.value =
		draft.value.server.port > 0 ? String(draft.value.server.port) : "";
	cdpPortInput.value =
		draft.value.browser.cdp_port > 0
			? String(draft.value.browser.cdp_port)
			: "";
	revertAvailable.value = true;
	flashSaveSuccess();
}

async function revertToLatestBackup() {
	if (loading.value || saving.value || reverting.value) return;
	reverting.value = true;
	errorMessage.value = "";
	okMessage.value = "";
	try {
		await callTool("config_restore_latest_backup");
		revertAvailable.value = false;
		await loadConfig();
		okMessage.value = "Settings reverted from latest backup.";
	} catch (e) {
		errorMessage.value = e instanceof Error ? e.message : String(e);
	} finally {
		reverting.value = false;
	}
}

async function loadSessions() {
	if (!sessionAgent.value) return;
	sessionLoading.value = true;
	errorMessage.value = "";
	try {
		const raw = await callTool("session_list", { agent: sessionAgent.value });
		sessions.value = safeJsonParse<SessionRow[]>(raw, []);
	} catch (e) {
		errorMessage.value = e instanceof Error ? e.message : String(e);
		sessions.value = [];
	} finally {
		sessionLoading.value = false;
	}
}

async function createSession() {
	if (!sessionAgent.value) return;
	try {
		await callTool("session_create", { agent: sessionAgent.value });
		await loadSessions();
	} catch (e) {
		errorMessage.value = e instanceof Error ? e.message : String(e);
	}
}

async function stopSession(sessionID: string) {
	try {
		await callTool("session_stop", {
			agent: sessionAgent.value,
			session_id: sessionID,
		});
		await loadSessions();
	} catch (e) {
		errorMessage.value = e instanceof Error ? e.message : String(e);
	}
}

async function confirmRemoveSession() {
	const sess = removeTarget.value;
	removeTarget.value = null;
	removeTargetOpen.value = false;
	if (!sess) return;
	try {
		await callTool("session_remove", {
			agent: sessionAgent.value,
			session_id: sess.id,
		});
		await loadSessions();
	} catch (e) {
		errorMessage.value = e instanceof Error ? e.message : String(e);
	}
}

function formatDate(value: string): string {
	if (!value) return "—";
	const date = new Date(value);
	if (Number.isNaN(date.getTime())) return value;
	return date.toLocaleString();
}

async function addProviderApiKey() {
	const provider = providerAddSelection.value.replace(/:apikey$/, "");
	const key = `${provider}:default`;
	const val = providerApiKeyValue.value.trim();
	if (!provider || !val) return;
	errorMessage.value = "";
	try {
		await callTool("auth_set", { name: key, value: val });
		providerApiKeyValue.value = "";
		providerAddSelection.value = "";
		await refreshCredentials();
		okMessage.value = `${provider} API key stored.`;
	} catch (e) {
		errorMessage.value = e instanceof Error ? e.message : String(e);
	}
}

async function addProviderOAuth() {
	if (!providerAddSelection.value) return;
	const authId = providerAddSelection.value.replace(/:oauth$/, "");
	const p = KNOWN_PROVIDERS.find((p) => p.authId === authId && p.hasOAuth);
	if (!p) return;
	// Show busy state and surface errors/success to the user.
	oauthBusy.value = true;
	errorMessage.value = "";
	okMessage.value = "";
	try {
		if (p.id === "anthropic") {
			await startAnthropic();
		} else if (p.id === "openai-codex") {
			await loginOpenAI();
			providerAddSelection.value = "";
		} else if (p.id === "google") {
			await loginGemini();
			providerAddSelection.value = "";
		} else if (p.id === "github-copilot") {
			await startCopilot();
			return; // startCopilot handles its own state
		}
		await refreshCredentials();
		okMessage.value = `${p.label} OAuth completed.`;
	} catch (e) {
		errorMessage.value = e instanceof Error ? e.message : String(e);
	} finally {
		oauthBusy.value = false;
	}
}

async function reauthorizeProvider(provider: string) {
	errorMessage.value = "";
	okMessage.value = "";
	oauthBusy.value = true;
	try {
		if (provider === "anthropic") {
			await startAnthropic();
		} else if (provider === "openai" || provider === "openai-codex") {
			await loginOpenAI();
		} else if (provider === "google") {
			await loginGemini();
		} else if (provider === "github-copilot") {
			await startCopilot();
			return; // startCopilot handles its own state
		}
		await refreshCredentials();
		okMessage.value = "Re-authorization completed.";
	} catch (e) {
		errorMessage.value = e instanceof Error ? e.message : String(e);
	} finally {
		oauthBusy.value = false;
	}
}

async function deleteProviderCredential(key: string) {
	errorMessage.value = "";
	try {
		await callTool("auth_delete", { name: key });
		await refreshCredentials();
		okMessage.value = "Provider credential removed.";
	} catch (e) {
		errorMessage.value = e instanceof Error ? e.message : String(e);
	}
}

async function addSecret() {
	const name = secretName.value.trim().replace(/^auth:/, "");
	if (!name) return;
	errorMessage.value = "";
	try {
		await callTool("auth_set", { name, value: secretValue.value });
		secretName.value = "";
		secretValue.value = "";
		await refreshCredentials();
		okMessage.value = `Secret stored: ${name}`;
	} catch (e) {
		errorMessage.value = e instanceof Error ? e.message : String(e);
	}
}

async function deleteSecret(name: string) {
	errorMessage.value = "";
	try {
		await callTool("auth_delete", { name });
		await refreshCredentials();
		okMessage.value = `Secret deleted: ${name}`;
	} catch (e) {
		errorMessage.value = e instanceof Error ? e.message : String(e);
	}
}

async function loginOpenAI() {
	oauthBusy.value = true;
	errorMessage.value = "";
	okMessage.value = "";
	try {
		const text = await callTool("auth_login_openai");
		okMessage.value = text || "OpenAI OAuth completed.";
		await refreshCredentials();
	} catch (e) {
		errorMessage.value = e instanceof Error ? e.message : String(e);
	} finally {
		oauthBusy.value = false;
	}
}

async function loginGemini() {
	oauthBusy.value = true;
	errorMessage.value = "";
	okMessage.value = "";
	try {
		const text = await callTool("auth_login_gemini");
		okMessage.value = text || "Gemini OAuth completed.";
		await refreshCredentials();
	} catch (e) {
		errorMessage.value = e instanceof Error ? e.message : String(e);
	} finally {
		oauthBusy.value = false;
	}
}

async function startCopilot() {
	oauthBusy.value = true;
	errorMessage.value = "";
	okMessage.value = "";
	copilotUserCode.value = "";
	copilotVerifyUrl.value = "";
	try {
		const raw = await callTool("auth_login_github_copilot");
		const parsed = JSON.parse(raw) as {
			user_code?: string;
			verification_uri?: string;
		};
		copilotUserCode.value = parsed.user_code ?? "";
		copilotVerifyUrl.value = parsed.verification_uri ?? "";
		okMessage.value =
			"Enter the code shown on GitHub's device authorization page.";
	} catch (e) {
		errorMessage.value = e instanceof Error ? e.message : String(e);
	} finally {
		oauthBusy.value = false;
	}
}

async function completeCopilot() {
	oauthBusy.value = true;
	errorMessage.value = "";
	okMessage.value = "";
	try {
		const text = await callTool("auth_login_github_copilot_complete");
		copilotUserCode.value = "";
		copilotVerifyUrl.value = "";
		providerAddSelection.value = "";
		okMessage.value = text || "GitHub Copilot login completed.";
		await refreshCredentials();
	} catch (e) {
		errorMessage.value = e instanceof Error ? e.message : String(e);
	} finally {
		oauthBusy.value = false;
	}
}

async function startAnthropic() {
	oauthBusy.value = true;
	errorMessage.value = "";
	okMessage.value = "";
	anthropicUrl.value = "";
	try {
		const raw = await callTool("auth_login_anthropic");
		const parsed = JSON.parse(raw) as { url?: string; instructions?: string };
		anthropicUrl.value = parsed.url ?? "";
		okMessage.value = parsed.instructions ?? "Anthropic OAuth started.";
	} catch (e) {
		errorMessage.value = e instanceof Error ? e.message : String(e);
	} finally {
		oauthBusy.value = false;
	}
}

async function completeAnthropic() {
	if (!anthropicCode.value.trim()) return;
	oauthBusy.value = true;
	errorMessage.value = "";
	okMessage.value = "";
	try {
		const text = await callTool("auth_login_anthropic_complete", {
			code: anthropicCode.value.trim(),
		});
		anthropicCode.value = "";
		anthropicUrl.value = "";
		providerAddSelection.value = "";
		okMessage.value = text || "Anthropic OAuth completed.";
		await refreshCredentials();
	} catch (e) {
		errorMessage.value = e instanceof Error ? e.message : String(e);
	} finally {
		oauthBusy.value = false;
	}
}
</script>

<style scoped>
@reference "../style.css";

.field-input {
	@apply w-full rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 placeholder-gray-400 focus:border-blue-500 focus:outline-none dark:border-gray-700 dark:bg-gray-800 dark:text-white dark:placeholder-gray-500;
}

.field-label {
	@apply mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400;
}

.danger-btn {
	@apply rounded-lg border border-red-200 px-3 py-2 text-xs font-medium text-red-600 hover:bg-red-50 dark:border-red-900 dark:text-red-400 dark:hover:bg-red-950;
}

.save-indicator-enter-active,
.save-indicator-leave-active {
	transition:
		opacity 180ms ease,
		transform 180ms ease;
}

.save-indicator-enter-from,
.save-indicator-leave-to {
	opacity: 0;
	transform: translateY(-4px);
}
</style>
