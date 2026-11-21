<template>
				<section v-show="activeTab === 'agents'"
					:class="isAgentFilesPage ? 'flex min-h-0 flex-1 flex-col' : 'space-y-5 pb-8'">
					<div v-if="!draft.agents.length"
						class="rounded-xl border border-gray-200 bg-white px-4 py-3 text-sm text-gray-500 dark:border-gray-800 dark:bg-gray-900 dark:text-gray-400">
						No agents configured.
						<button type="button" data-testid="empty-add-agent"
							class="ml-2 font-medium text-blue-600 hover:underline dark:text-blue-400" @click="addAgent">+ Add Agent</button>
					</div>

					<div v-for="{ agent, i } in selectedAgentAsSingletonList" :key="`agent-${i}`"
						:class="isAgentFilesPage ? 'flex min-h-0 flex-1 flex-col' : 'space-y-5'">
						<!-- Agent Settings is one page of stacked sections; Files, Tasks and
						     Code Loop are pages of their own in the sidebar. -->
						<section v-show="onAgentSettingsPage" id="agent-settings-general" data-agent-settings-section="general"
							aria-labelledby="agent-settings-general-heading" class="scroll-mt-24 rounded-xl border border-gray-200 bg-white dark:border-gray-800 dark:bg-gray-900">
							<h3 id="agent-settings-general-heading" class="border-b border-gray-200 px-5 py-3 text-sm font-semibold text-gray-800 dark:border-gray-700 dark:text-gray-200">General</h3>
							<div class="space-y-4 p-5">
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

						</section>

						<!-- Files page: the sidebar lists the files; the page is the open
						     file's editor, under one bar with its name and actions. -->
						<div v-show="selectedAgentSubtab === 'files'" class="flex min-h-0 flex-1 flex-col">
							<div data-testid="agent-file-bar"
								class="flex items-center justify-between gap-3 border-b border-gray-200/80 bg-white/90 px-4 pb-4 pt-10 backdrop-blur sm:px-6 dark:border-gray-800/80 dark:bg-gray-950/88">
								<h2 class="flex min-w-0 items-center gap-2 text-xl font-bold text-gray-900 dark:text-white">
									<span data-testid="agent-file-name" class="truncate font-mono">{{ getAgentFileState(agent.name).selectedFile || 'Files' }}</span>
									<span v-if="isProtectedAgentFile(getAgentFileState(agent.name).selectedFile)" data-testid="agent-file-builtin"
										class="shrink-0 rounded bg-gray-200 px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-gray-700 dark:bg-gray-700 dark:text-gray-200">Built-in</span>
									<span data-testid="agent-file-hint" role="img" :aria-label="agentFileHint" :title="agentFileHint"
										class="shrink-0 cursor-help text-gray-400 hover:text-gray-600 dark:text-gray-500 dark:hover:text-gray-300">
										<svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
											<path fill-rule="evenodd"
												d="M18 10a8 8 0 1 1-16 0 8 8 0 0 1 16 0Zm-7-4a1 1 0 1 1-2 0 1 1 0 0 1 2 0ZM9 9a.75.75 0 0 0 0 1.5h.253a.25.25 0 0 1 .244.304l-.459 2.066A1.75 1.75 0 0 0 10.747 15H11a.75.75 0 0 0 0-1.5h-.253a.25.25 0 0 1-.244-.304l.459-2.066A1.75 1.75 0 0 0 9.253 9H9Z"
												clip-rule="evenodd" />
										</svg>
									</span>
								</h2>
								<div class="flex shrink-0 items-center gap-2">
									<transition name="save-indicator">
										<div v-if="getAgentFileState(agent.name).saveFlash"
											class="flex items-center gap-1.5 text-emerald-600 dark:text-emerald-400">
											<svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" viewBox="0 0 20 20" fill="currentColor"
												aria-hidden="true">
												<path fill-rule="evenodd"
													d="M10 18a8 8 0 1 0 0-16 8 8 0 0 0 0 16Zm3.78-9.72a.75.75 0 0 0-1.06-1.06L9.25 10.69 7.78 9.22a.75.75 0 1 0-1.06 1.06l2 2a.75.75 0 0 0 1.06 0l4-4Z"
													clip-rule="evenodd" />
											</svg>
											<span class="text-xs font-medium">Saved</span>
										</div>
									</transition>
									<FilePreviewToggle :file-name="getAgentFileState(agent.name).selectedFile" />
									<DropdownMenuRoot>
										<DropdownMenuTrigger asChild>
											<button type="button" data-testid="agent-file-menu" aria-label="More file actions" title="More file actions"
												class="inline-flex h-9 w-9 items-center justify-center rounded-lg border border-gray-200 text-gray-600 hover:bg-gray-100 disabled:opacity-50 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800"
												:disabled="!agent.name">
												<svg viewBox="0 0 16 16" fill="currentColor" aria-hidden="true" class="h-4 w-4">
													<circle cx="3" cy="8" r="1.4" /><circle cx="8" cy="8" r="1.4" /><circle cx="13" cy="8" r="1.4" />
												</svg>
											</button>
										</DropdownMenuTrigger>
										<DropdownMenuContent align="end" :side-offset="4"
											class="z-50 min-w-40 rounded-xl border border-gray-200 bg-white p-1 shadow-lg dark:border-gray-700 dark:bg-gray-900">
											<DropdownMenuItem v-for="action in agentFileMenuActions(agent.name)" :key="action.key"
												:disabled="action.disabled" :title="action.title"
												class="flex cursor-pointer items-center gap-2 rounded-lg px-3 py-2 text-sm outline-none data-[disabled]:cursor-default data-[disabled]:opacity-40"
												:class="action.danger
													? 'text-red-600 data-[highlighted]:bg-red-50 dark:text-red-400 dark:data-[highlighted]:bg-red-950/40'
													: 'text-gray-700 data-[highlighted]:bg-gray-100 dark:text-gray-200 dark:data-[highlighted]:bg-gray-800'"
												@select="runAgentFileMenuAction(agent.name, action.key)">
												<component :is="action.icon" aria-hidden="true" class="h-4 w-4" />
												{{ action.label }}
											</DropdownMenuItem>
										</DropdownMenuContent>
									</DropdownMenuRoot>
									<button type="button"
										class="rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white hover:bg-blue-500 disabled:opacity-50"
										:disabled="!agent.name || !getAgentFileState(agent.name).selectedFile || getAgentFileState(agent.name).saving"
										@click="saveAgentFile(agent.name)">
										{{ getAgentFileState(agent.name).saving ? 'Saving…' : 'Save' }}
									</button>
								</div>
							</div>
							<p v-if="getAgentFileState(agent.name).error"
								class="border-b border-red-200 bg-red-50 px-4 py-2 text-xs text-red-700 sm:px-6 dark:border-red-900 dark:bg-red-950 dark:text-red-300">
								{{ getAgentFileState(agent.name).error }}
							</p>
							<FilePreviewSplit :file-name="getAgentFileState(agent.name).selectedFile"
								:content="getAgentFileState(agent.name).content">
								<textarea data-testid="agent-file-editor" :value="getAgentFileState(agent.name).content"
									@input="getAgentFileState(agent.name).content = ($event.target as HTMLTextAreaElement).value"
									@keydown="(e: KeyboardEvent) => { if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 's') { e.preventDefault(); e.stopPropagation(); void saveAgentFile(agent.name); } }"
									class="min-h-0 w-full flex-1 resize-none overflow-y-auto border-0 bg-white px-4 py-4 font-mono text-sm leading-6 text-gray-900 placeholder-gray-400 focus:outline-none focus:ring-0 disabled:opacity-60 sm:px-6 dark:bg-gray-950 dark:text-gray-100 dark:placeholder-gray-500"
									:disabled="!agent.name || !getAgentFileState(agent.name).selectedFile"
									:placeholder="agent.name ? 'Pick a file in the sidebar, or add one with its +.' : 'Name the agent first to manage files.'" />
							</FilePreviewSplit>
						</div>

						<section v-show="onAgentSettingsPage" id="agent-settings-permissions" data-agent-settings-section="permissions"
							aria-labelledby="agent-settings-permissions-heading" class="scroll-mt-24 rounded-xl border border-gray-200 bg-white dark:border-gray-800 dark:bg-gray-900">
							<h3 id="agent-settings-permissions-heading" class="border-b border-gray-200 px-5 py-3 text-sm font-semibold text-gray-800 dark:border-gray-700 dark:text-gray-200">Permissions</h3>
							<div class="space-y-4 p-5">
							<div class="grid gap-3 lg:max-w-xl">
								<div>
									<label class="field-label" :for="`tool-preset-${agent.name || i}`">Tool preset</label>
									<AppSelect :id="`tool-preset-${agent.name || i}`" :model-value="agentPermissionsPreset(agent)"
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
						</section>

						<section v-if="selectedAgentSubtab === 'channels'" id="agent-settings-channels" data-agent-settings-section="channels"
							data-testid="channel-settings"
							aria-labelledby="agent-settings-channels-heading" class="scroll-mt-24 rounded-xl border border-gray-200 bg-white dark:border-gray-800 dark:bg-gray-900">
							<div class="flex items-center justify-between border-b border-gray-200 px-5 py-2 dark:border-gray-700">
								<h3 id="agent-settings-channels-heading" class="py-1 text-sm font-semibold text-gray-800 dark:text-gray-200">Channel</h3>
								<router-link v-if="selectedChannel" :to="channelPath(agent.name, channelName(selectedChannel))"
									data-testid="channel-open-conversation"
									class="rounded-lg border border-gray-200 px-3 py-1.5 text-xs text-gray-700 hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800">Open conversation</router-link>
							</div>
							<div class="space-y-4 p-5">
							<p v-if="channelNotFound || !selectedChannel"
								class="rounded-lg border border-dashed border-gray-300 px-3 py-6 text-sm text-gray-500 dark:border-gray-700 dark:text-gray-400">
								No channel named {{ $route.params.channel }}.
							</p>
							<template v-else>
							<template v-for="(ch, k) in agent.channels" :key="`ch-${i}-${k}`">
							<div v-if="selectedChannelIdx === k"
								class="space-y-3 rounded-lg border p-4 transition" :class="channelCardClass(ch)">
								<div class="flex flex-wrap items-center justify-between gap-3">
									<div class="flex items-center gap-2">
										<span :class="channelTypeIconClass(ch)">
											<MessagingLogo :name="ch.type" :disabled="!isChannelEnabled(ch)" class="h-3.5 w-3.5" />
											<span class="sr-only">{{ channelTypeLabel(ch) }}</span>
										</span>
										<h5 class="text-sm font-semibold text-gray-800 dark:text-gray-200">{{ channelName(ch) }}</h5>
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
									</div>
								</div>
								<p v-if="!isChannelEnabled(ch)" class="text-xs text-gray-500 dark:text-gray-400">
									Disabled channels are not started and will not receive or send messages until re-enabled.
								</p>
								<div class="grid gap-3 lg:grid-cols-[160px_1fr]">
									<div>
										<label class="field-label">Type</label>
										<AppSelect v-model="ch.type" :options="CHANNEL_TYPE_OPTIONS" aria-label="Type" />
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
										<label class="field-label" for="channel-deliver-to">Deliver to</label>
										<input id="channel-deliver-to" v-model="ch.deliver_to" data-testid="channel-deliver-to" type="text"
											class="field-input" :placeholder="channelDeliverToPlaceholder(ch)" />
										<p class="mt-1 text-xs text-gray-400 dark:text-gray-500">
											Scheduled tasks, project reports and other deliveries pick this channel by name. They land in its
											conversation here and are sent to this {{ ch.type === 'signal' ? 'number or group' : 'channel' }}.
										</p>
									</div>
									<div>
										<span class="field-label">Agent</span>
										<p class="py-2 text-sm text-gray-700 dark:text-gray-300" data-testid="channel-owner">{{ agent.name }}</p>
										<p class="text-xs text-gray-400 dark:text-gray-500">The channel's messages run as this agent.</p>
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
										<SecretSelect
											v-model="ch.url"
											:secrets="webSearchSecretOptions"
											:data-testid="`channel-secret-${agent.name || i}-${k}-url`"
											@add-secret="openChannelTokenSecretModal(ch, 'url')"
										/>
									</div>
									<div>
										<label class="field-label">Bot Token (xoxb-…)</label>
										<SecretSelect
											v-model="ch.token"
											:secrets="webSearchSecretOptions"
											:data-testid="`channel-secret-${agent.name || i}-${k}-token`"
											@add-secret="openChannelTokenSecretModal(ch, 'token')"
										/>
									</div>
									<label class="lg:col-span-2 flex cursor-pointer items-center gap-2 text-xs text-gray-600 dark:text-gray-400">
										<input type="checkbox" v-model="ch.separate_top_level_sessions"
											class="h-3.5 w-3.5 rounded border-gray-300 text-blue-600 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800" />
										Separate top-level Slack messages into thread sessions
									</label>
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
										<SecretSelect
											v-model="ch.token"
											:secrets="webSearchSecretOptions"
											:data-testid="`channel-secret-${agent.name || i}-${k}-token`"
											@add-secret="openChannelTokenSecretModal(ch, 'token')"
										/>
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
							</template>
							</template>
							</div>
						</section>

						<section v-show="onAgentSettingsPage" data-testid="agent-danger-zone" aria-labelledby="agent-danger-zone-heading"
							class="rounded-xl border border-red-200 bg-white dark:border-red-900/60 dark:bg-gray-900">
							<h3 id="agent-danger-zone-heading" class="border-b border-red-200 px-5 py-3 text-sm font-semibold text-red-700 dark:border-red-900/60 dark:text-red-400">Danger Zone</h3>
							<div class="flex flex-wrap items-center justify-between gap-4 p-5">
								<p class="text-sm text-gray-600 dark:text-gray-400">Remove this agent and all of its settings from the configuration.</p>
								<button type="button"
									class="rounded-lg border border-red-300 px-3 py-1.5 text-sm font-medium text-red-600 hover:bg-red-50 dark:border-red-800 dark:text-red-400 dark:hover:bg-red-950/40"
									@click="removeAgent(i)">
									Remove Agent
								</button>
							</div>
						</section>

						<!-- Tasks page: the overview, or the open task's editor -->
						<TasksOverview v-if="selectedAgentSubtab === 'tasks' && tasksRoot" :agent="agent.name" />
						<div v-show="selectedAgentSubtab === 'tasks' && !tasksRoot"
							class="min-h-[60vh] space-y-4 rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
							<div class="flex items-center justify-between">
								<h4 class="text-sm font-semibold text-gray-800 dark:text-gray-200">Scheduled Tasks</h4>
							</div>



<!-- The sidebar lists the tasks; this edits the open one. -->
<div>
<div v-if="selectedTask">
								<div class="mb-3 flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
									<div class="min-w-0">
										<div class="flex flex-wrap items-center gap-2 sm:gap-3">
											<span v-if="!isTaskEnabled(selectedTask)" class="flex-shrink-0 rounded-full bg-red-50 px-3 py-1 text-sm font-medium text-red-700 dark:bg-red-900/20 dark:text-red-300">disabled</span>
											<span class="flex-shrink-0 rounded-full px-3 py-1 text-sm font-medium"
												:class="selectedTask.type === 'script' ? 'bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-300' : selectedTask.type === 'project' ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300' : 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300'">
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

								<div class="grid gap-3 lg:grid-cols-2">
<div>
<label class="field-label">Task name</label>
<input v-model="selectedTask.name" type="text" class="field-input font-mono" placeholder="daily-briefing"
	@focus="onTaskNameFocus" @blur="onTaskNameBlur" :disabled="renamingTask" />
</div>
<div>
<label class="field-label">Task type</label>
<AppSelect v-model="selectedTask.type" :options="TASK_TYPE_OPTIONS" aria-label="Task type" />
</div>
</div>

<div class="mt-3">
<TriggerEditor :key="`${selectedAgentIdx}:${selectedTaskIdx}`" :model-value="taskTrigger(selectedTask)" :preview="previewTrigger" optional
	@update:model-value="setTaskTrigger(selectedTask, $event)" />
</div>

<div v-if="selectedTask.type === 'project'" class="grid gap-3 lg:grid-cols-2 mt-3">
<div>
<label class="field-label">Project</label>
<AppCombobox v-model="selectedTask.project" aria-label="Project" placeholder="Select a watched project…"
	empty-text="No matching projects" :options="projectTargetOptions(selectedTask).map((t: string) => ({ value: t, label: t }))" />
<router-link v-if="selectedTask.project" data-testid="task-project-schedule-link"
	:to="projectScheduleLink(agent.name, selectedTask)"
	class="mt-1 inline-block text-xs text-blue-600 hover:underline dark:text-blue-400">Open in the project's schedules</router-link>
</div>
<div data-testid="task-model">
<label class="field-label">Model override (optional)</label>
<ModelSelector :model-value="selectedTask.model ?? ''" :options="availableModelOptions"
placeholder="Default project agent model"
@update:model-value="selectedTask.model = typeof $event === 'string' ? ($event || undefined) : undefined" />
</div>
</div>

<div class="grid gap-3 lg:grid-cols-2 mt-3">
<div v-if="selectedTask.type !== 'project'">
<label class="field-label">Watch</label>
<input v-model="selectedTask.watch" type="text" class="field-input" placeholder="./docs/**/*.md" />
</div>
<div>
<label class="field-label">Send Via</label>
<AppSelect :model-value="taskChannelSelection(selectedTask)" aria-label="Send Via" data-testid="task-send-via"
	:options="[{ value: '', label: 'silent' }, ...configuredTaskChannelOptions(agent)]"
	@update:model-value="setTaskChannelSelection(selectedTask, $event)" />
</div>
</div>

<div v-if="taskChannelSelection(selectedTask) === 'session'" class="grid gap-3 lg:grid-cols-2 mt-3">
<div>
<label class="field-label">Session</label>
<SessionSelect
	:agent="agent.name"
	:model-value="selectedTaskTarget(selectedTask)"
	aria-label="Session"
	@update:model-value="setSelectedTaskTarget(selectedTask, $event)"
/>
</div>
</div>

<div class="mt-4">
<label class="field-label">{{ selectedTask.type === 'script' ? 'Script' : 'Prompt' }}</label>
<textarea v-model="selectedTask.prompt" rows="12" class="field-input min-h-[28vh] font-mono text-xs" :placeholder="selectedTask.type === 'script' ? 'print(\'hello from lua\')' : 'Task prompt...'"></textarea>
<TaskCostFeedback :agent="agent.name" :prompt="selectedTask.prompt ?? ''" :type="selectedTask.type || 'prompt'"
	:target="selectedTask.target ?? ''" :model="selectedTask.type === 'project' ? (selectedTask.model ?? '') : ''" />
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

						<section v-if="selectedAgentSubtab === 'tasks' && selectedTask?.name" data-testid="task-danger-zone" aria-labelledby="task-danger-zone-heading"
							class="rounded-xl border border-red-200 bg-white dark:border-red-900/60 dark:bg-gray-900">
							<h3 id="task-danger-zone-heading" class="border-b border-red-200 px-5 py-3 text-sm font-semibold text-red-700 dark:border-red-900/60 dark:text-red-400">Danger Zone</h3>
							<div class="flex flex-wrap items-center justify-between gap-4 p-5">
								<p class="text-sm text-gray-600 dark:text-gray-400">
									Remove this scheduled task from the agent's configuration<template v-if="selectedTask.from_file">, including its definition file {{ taskDefinedIn(selectedTask) }}</template>. It will no longer run.
								</p>
								<button type="button"
									class="rounded-lg border border-red-300 px-3 py-1.5 text-sm font-medium text-red-600 hover:bg-red-50 dark:border-red-800 dark:text-red-400 dark:hover:bg-red-950/40"
									@click="removeTask(agent.name, selectedTask.name)">
									Remove Task
								</button>
							</div>
						</section>
					</div>
				</section>
</template>

<script lang="ts">
import ArrowPathIcon from "@heroicons/vue/24/outline/ArrowPathIcon";
import DocumentDuplicateIcon from "@heroicons/vue/24/outline/DocumentDuplicateIcon";
import PencilIcon from "@heroicons/vue/24/outline/PencilIcon";
import TrashIcon from "@heroicons/vue/24/outline/TrashIcon";
import {
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuRoot,
	DropdownMenuTrigger,
	SwitchRoot,
	SwitchThumb,
} from "radix-vue";
import { type Component, defineComponent, inject } from "vue";
import { useAgentItemDialogs } from "../../composables/useAgentItemDialogs";
import { channelName, channelPath } from "../../lib/channels";
import { projectOwner } from "../../lib/projectSettings";
import { projectPath } from "../../lib/projects";
import { type AgentTask, useSettingsStore } from "../../stores/settings";
import FilePreviewSplit from "../files/FilePreviewSplit.vue";
import FilePreviewToggle from "../files/FilePreviewToggle.vue";
import MessagingLogo from "../MessagingLogo.vue";
import ModelSelector from "../ModelSelector.vue";
import SecretSelect from "../SecretSelect.vue";
import SessionSelect from "../SessionSelect.vue";
import TaskCostFeedback from "../TaskCostFeedback.vue";
import TriggerEditor from "../TriggerEditor.vue";
import TasksOverview from "../tasks/TasksOverview.vue";
import { AppCombobox, AppSelect } from "../ui/select";
import { settingsViewContextKey } from "./context";

export default defineComponent({
	name: "SettingsAgentsTab",
	components: {
		DropdownMenuContent,
		DropdownMenuItem,
		DropdownMenuRoot,
		DropdownMenuTrigger,
		AppCombobox,
		AppSelect,
		FilePreviewSplit,
		FilePreviewToggle,
		MessagingLogo,
		ModelSelector,
		SecretSelect,
		SessionSelect,
		SwitchRoot,
		SwitchThumb,
		TasksOverview,
		TaskCostFeedback,
		TriggerEditor,
	},
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
					description: option.description,
				}),
			);
		const permissionPresetDescription = (agent: unknown) =>
			settings.PERMISSION_PRESET_OPTIONS.find(
				(option: { value: string; label: string; description: string }) =>
					option.value === settings.agentPermissionsPreset(agent),
			)?.description;
		const CHANNEL_TYPE_OPTIONS = ["slack", "discord", "signal"].map((t) => ({
			value: t,
			label: t,
		}));
		const TASK_TYPE_OPTIONS = [
			{ value: "prompt", label: "Prompt" },
			{ value: "script", label: "Script" },
			{ value: "project", label: "Project" },
		];
		const agentFileHint =
			"Root-level .md only. AGENTS.md, SYSTEM.md, MEMORY.md, and RULES.md are built in and cannot be renamed or deleted.";
		const store = useSettingsStore();
		const { open: openItemDialog } = useAgentItemDialogs();
		// The Files page's "..." menu, left of Save. Rename and Delete act on
		// the open file through the sidebar's dialogs.
		const agentFileMenuActions = (agentName: string) => {
			const state = settings.getAgentFileState(agentName);
			const file = state.selectedFile;
			const locked = settings.isProtectedAgentFile(file);
			const fileTitle = !file
				? "Open a file first"
				: locked
					? "Built-in files cannot be renamed or deleted"
					: undefined;
			const actions: {
				key: string;
				label: string;
				icon: Component;
				disabled: boolean;
				title?: string;
				danger?: boolean;
			}[] = [
				{
					key: "refresh",
					label: state.loading ? "Loading…" : "Refresh",
					icon: ArrowPathIcon,
					disabled: state.loading,
				},
				{
					key: "sync",
					label: state.syncing ? "Syncing…" : "Sync Templates",
					icon: DocumentDuplicateIcon,
					disabled: state.syncing,
				},
				{
					key: "rename",
					label: "Rename",
					icon: PencilIcon,
					disabled: !file || locked,
					title: fileTitle,
				},
				{
					key: "delete",
					label: "Delete",
					icon: TrashIcon,
					disabled: !file || locked,
					title: fileTitle,
					danger: true,
				},
			];
			return actions;
		};
		const runAgentFileMenuAction = (agentName: string, key: string) => {
			const file = settings.getAgentFileState(agentName).selectedFile;
			if (key === "refresh") void settings.refreshAgentFiles(agentName);
			else if (key === "sync") void settings.syncAgentTemplates(agentName);
			else if (file)
				openItemDialog({
					kind: "file",
					agent: agentName,
					mode: key === "delete" ? "delete" : "edit",
					name: file,
				});
		};
		// A project scheduled task is also edited from its project's board.
		const projectScheduleLink = (agentName: string, task: AgentTask) => ({
			path: projectPath(
				projectOwner(store.config, task.project ?? ""),
				task.project ?? "",
			),
			query: { scheduled: `${agentName}/${task.name}` },
		});
		// The sidebar's delete dialog confirms and removes the task.
		const removeTask = (agent: string, name: string) =>
			openItemDialog({ kind: "task", agent, mode: "delete", name });
		const channelDeliverToPlaceholder = (ch: { type: string }) => {
			if (ch.type === "slack") return "C1234567890";
			if (ch.type === "discord") return "123456789012345678";
			if (ch.type === "signal") return "+15551234567 or group ID";
			return "Conversation ID";
		};
		return Object.assign(settings, {
			channelDeliverToPlaceholder,
			channelName,
			channelPath,
			agentFileHint,
			CHANNEL_TYPE_OPTIONS,
			TASK_TYPE_OPTIONS,
			agentFileMenuActions,
			runAgentFileMenuAction,
			projectScheduleLink,
			permissionPresetDescription,
			permissionPresetOptions,
			removeTask,
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
