<template>
				<section v-show="activeTab === 'agents'" class="space-y-5 pb-8">
					<div class="flex items-center border-b border-gray-200 dark:border-gray-800">
						<div class="scrollbar-none flex flex-1 items-end overflow-x-auto">
							<button v-for="(a, idx) in draft.agents" :key="`tab-${idx}`" type="button"
								class="-mb-px shrink-0 border-b-2 px-4 py-2.5 text-sm transition-colors"
								:class="selectedAgentIdx === idx
									? 'border-blue-600 font-semibold text-blue-700 dark:border-blue-400 dark:text-blue-400'
									: 'border-transparent font-medium text-gray-500 hover:border-gray-300 hover:text-gray-800 dark:text-gray-400 dark:hover:border-gray-600 dark:hover:text-gray-200'"
								@click="selectedAgentIdx = idx">
								{{ a.name || `Agent ${Number(idx) + 1}` }}
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
										:options="permissionPresetOptions()"
										@update:model-value="updateAgentPermissionsPreset(agent, $event)" />
									<p class="mt-1 text-xs text-gray-400 dark:text-gray-500">
										{{ permissionPresetDescription(agent) }}
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
										<h5 class="text-sm font-semibold text-gray-800 dark:text-gray-200">Channel {{ Number(k) + 1 }}</h5>
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
													{{ Number(ei) + 1 }}</span>
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
<span class="truncate">{{ task.name || `Task ${Number(j) + 1}` }}</span>
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
</template>

<script lang="ts">
import { SwitchRoot, SwitchThumb } from "radix-vue";
import { defineComponent, inject } from "vue";
import FancySelect from "../FancySelect.vue";
import ModelSelector from "../ModelSelector.vue";
import { settingsViewContextKey } from "./context";

export default defineComponent({
	name: "SettingsAgentsTab",
	components: { FancySelect, ModelSelector, SwitchRoot, SwitchThumb },
	setup() {
		const settings = inject(settingsViewContextKey);
		if (!settings) {
			throw new Error("Settings view context is not available.");
		}
		const permissionPresetOptions = () =>
			settings.PERMISSION_PRESET_OPTIONS.map(
				(option: { value: string; label: string; description: string }) => ({
					value: option.value,
					label: option.label,
					caption: option.description,
				}),
			);
		const permissionPresetDescription = (agent: unknown) =>
			settings.PERMISSION_PRESET_OPTIONS.find(
				(option: { value: string; label: string; description: string }) =>
					option.value === settings.agentPermissionsPreset(agent),
			)?.description;
		return Object.assign(settings, {
			permissionPresetDescription,
			permissionPresetOptions,
		});
	},
});
</script>

<style scoped>
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

