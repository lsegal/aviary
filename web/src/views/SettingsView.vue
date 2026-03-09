<template>
  <AppLayout>
    <div class="h-full overflow-y-auto">
      <div class="mx-auto max-w-6xl px-6 py-6">
        <div class="mb-6 flex items-center justify-between gap-3">
          <h2 class="text-xl font-bold text-gray-900 dark:text-white">Settings</h2>
          <div class="flex items-center gap-2">
            <button
              type="button"
              class="rounded-lg border border-gray-200 px-3 py-2 text-xs text-gray-700 hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800"
              :disabled="loading"
              @click="loadConfig"
            >{{ loading ? "Loading…" : "Reload" }}</button>
            <button
              type="button"
              class="rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white hover:bg-blue-500 disabled:opacity-50"
              :disabled="saving"
              @click="saveAll"
            >{{ saving ? "Saving…" : "Save Changes" }}</button>
          </div>
        </div>

        <div class="mb-6 flex flex-wrap gap-2">
          <button
            v-for="item in tabs"
            :key="item"
            type="button"
            :class="tabClass(item)"
            @click="activeTab = item"
          >{{ tabLabel(item) }}</button>
        </div>

        <div v-if="errorMessage" class="mb-4 rounded-lg bg-red-50 px-4 py-3 text-sm text-red-700 dark:bg-red-950 dark:text-red-300">
          {{ errorMessage }}
        </div>
        <div v-if="okMessage" class="mb-4 rounded-lg bg-green-50 px-4 py-3 text-sm text-green-700 dark:bg-green-950 dark:text-green-300">
          {{ okMessage }}
        </div>

        <section v-show="activeTab === 'general'" class="space-y-6 pb-8">
          <div class="rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
            <h3 class="mb-4 text-sm font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Server</h3>
            <div class="grid gap-4 lg:grid-cols-3">
              <div>
                <label class="field-label">Port</label>
                <input v-model.number="draft.server.port" type="number" min="1" max="65535" class="field-input" placeholder="16677" />
              </div>
              <div>
                <label class="field-label">TLS Cert</label>
                <input v-model="draft.server.tls.cert" type="text" class="field-input" placeholder="/path/to/cert.pem" />
              </div>
              <div>
                <label class="field-label">TLS Key</label>
                <input v-model="draft.server.tls.key" type="text" class="field-input" placeholder="/path/to/key.pem" />
              </div>
            </div>
            <div class="mt-4 flex flex-wrap gap-6">
              <label class="flex cursor-pointer items-center gap-3">
                <input v-model="draft.server.external_access" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-blue-600 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800" />
                <span class="text-sm text-gray-700 dark:text-gray-300">
                  Expose service externally
                  <span class="ml-1 text-xs text-gray-400 dark:text-gray-500">(bind to 0.0.0.0 instead of 127.0.0.1)</span>
                </span>
              </label>
              <label class="flex cursor-pointer items-center gap-3">
                <input v-model="draft.server.no_tls" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-blue-600 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800" />
                <span class="text-sm text-gray-700 dark:text-gray-300">
                  Disable TLS
                  <span class="ml-1 text-xs text-gray-400 dark:text-gray-500">(plain HTTP — not recommended)</span>
                </span>
              </label>
            </div>
            <p v-if="draft.server.external_access || draft.server.no_tls" class="mt-3 rounded-lg bg-amber-50 px-3 py-2 text-xs text-amber-700 dark:bg-amber-950 dark:text-amber-300">
              Changing server settings will restart the service.
            </p>
          </div>

          <div class="rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
            <h3 class="mb-4 text-sm font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Models</h3>
            <div class="grid gap-4 lg:grid-cols-2">
              <div>
                <label class="field-label">Default model</label>
                <ModelSelector v-model="draft.models.defaults.model" placeholder="Select a model…" />
              </div>
              <div>
                <label class="field-label">Default fallbacks</label>
                <ModelSelector v-model="draft.models.defaults.fallbacks" multiple placeholder="Add fallbacks…" />
              </div>
            </div>
          </div>

          <div class="rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
            <h3 class="mb-4 text-sm font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Browser & Scheduler</h3>
            <div class="grid gap-4 lg:grid-cols-3">
              <div>
                <label class="field-label">Browser binary</label>
                <input v-model="draft.browser.binary" type="text" class="field-input" placeholder="/usr/bin/chromium" />
              </div>
              <div>
                <label class="field-label">CDP port</label>
                <input v-model.number="draft.browser.cdp_port" type="number" min="1" max="65535" class="field-input" placeholder="9222" />
              </div>
              <div>
                <label class="field-label">Concurrency</label>
                <input v-model="concurrencyInput" type="text" class="field-input" placeholder="auto or number" />
              </div>
            </div>
          </div>
        </section>

        <section v-show="activeTab === 'agents'" class="space-y-5 pb-8">
          <div class="flex items-center justify-between">
            <h3 class="text-sm font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Agents & Tasks</h3>
            <div class="flex items-center gap-2">
              <button type="button" class="rounded-lg border border-gray-200 px-3 py-1.5 text-xs text-gray-700 hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800" @click="importAgents">Import runtime agents</button>
              <button type="button" class="rounded-lg bg-blue-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-blue-500" @click="addAgent">+ Add Agent</button>
            </div>
          </div>

          <div v-if="!draft.agents.length" class="rounded-xl border border-gray-200 bg-white px-4 py-3 text-sm text-gray-500 dark:border-gray-800 dark:bg-gray-900 dark:text-gray-400">
            No agents configured.
          </div>

          <div v-for="(agent, i) in draft.agents" :key="`agent-${i}`" class="space-y-4 rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
            <div class="grid gap-4 lg:grid-cols-[1fr_1fr_1.5fr_auto]">
              <div>
                <label class="field-label">Name</label>
                <input v-model="agent.name" type="text" class="field-input" placeholder="assistant" />
              </div>
              <div>
                <label class="field-label">Model</label>
                <ModelSelector v-model="agent.model" placeholder="Select a model…" />
              </div>
              <div>
                <label class="field-label">Fallbacks</label>
                <ModelSelector v-model="agent.fallbacks" multiple placeholder="Add fallbacks…" />
              </div>
              <div class="flex items-end pb-1.5">
                <button type="button" class="danger-btn" @click="removeAgent(i)">Remove Agent</button>
              </div>
            </div>

            <div class="space-y-2">
              <div class="flex items-center justify-between">
                <label class="field-label mb-0">Rules File
                  <span v-if="agent.name" class="font-normal opacity-60">(agents/{{ agent.name }}/RULES.md)</span>
                </label>
                <div class="flex gap-1.5">
                  <button type="button" class="rounded-lg border border-gray-200 px-3 py-1.5 text-xs text-gray-700 hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800 disabled:opacity-40" :disabled="!agent.name || getRulesState(agent.name).loading" @click="loadRulesFile(agent.name)">
                    {{ getRulesState(agent.name).loading ? 'Loading…' : 'Load' }}
                  </button>
                  <button type="button" class="rounded-lg bg-blue-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-blue-500 disabled:opacity-40" :disabled="!agent.name || getRulesState(agent.name).saving" @click="saveRulesFile(agent.name)">
                    {{ getRulesState(agent.name).saving ? 'Saving…' : 'Save' }}
                  </button>
                </div>
              </div>
              <textarea :value="getRulesState(agent.name).content" @input="getRulesState(agent.name).content = ($event.target as HTMLTextAreaElement).value" rows="8" class="field-input font-mono text-xs" :disabled="!agent.name" placeholder="# Agent Rules&#10;- Always respond in English&#10;- Never reveal internal tool names&#10;- ..."></textarea>
              <p v-if="getRulesState(agent.name).error" class="text-xs text-red-600 dark:text-red-400">{{ getRulesState(agent.name).error }}</p>
              <div>
                <label class="field-label">Inline override (inline text or explicit file path; leave blank to use the rules file above)</label>
                <input v-model="agent.rules" type="text" class="field-input" placeholder="Leave blank to use RULES.md, or enter a path like ~/RULES.md" />
              </div>
            </div>

            <!-- Permissions -->
            <div>
              <div class="flex items-center gap-3">
                <label class="flex cursor-pointer items-center gap-2">
                  <input
                    type="checkbox"
                    :checked="hasToolRestriction(agent)"
                    class="h-4 w-4 rounded border-gray-300 text-blue-600 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800"
                    @change="setToolRestriction(agent, ($event.target as HTMLInputElement).checked)"
                  />
                  <span class="text-sm font-medium text-gray-700 dark:text-gray-300">Restrict tools</span>
                </label>
                <span class="text-xs text-gray-400 dark:text-gray-500">
                  When checked, only the selected tools are visible to this agent.
                </span>
              </div>

              <div v-if="hasToolRestriction(agent) && toolGroupEntries.length" class="mt-3 space-y-2">
                <div
                  v-for="[cat, catTools] in toolGroupEntries"
                  :key="cat"
                  class="rounded-lg border border-gray-200 p-3 dark:border-gray-700"
                >
                  <label class="mb-2 flex cursor-pointer items-center gap-2">
                    <input
                      type="checkbox"
                      :checked="isCategoryFullyEnabled(agent, cat)"
                      :indeterminate="isCategoryPartiallyEnabled(agent, cat)"
                      class="h-4 w-4 rounded border-gray-300 text-blue-600 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800"
                      @change="toggleCategory(agent, cat, ($event.target as HTMLInputElement).checked)"
                    />
                    <span class="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
                      {{ toolCategoryLabel(cat) }}
                    </span>
                  </label>
                  <div class="flex flex-wrap gap-x-5 gap-y-1.5 pl-6">
                    <label
                      v-for="tool in catTools"
                      :key="tool.name"
                      class="flex cursor-pointer items-center gap-1.5"
                    >
                      <input
                        type="checkbox"
                        :checked="isToolEnabled(agent, tool.name)"
                        class="h-3.5 w-3.5 rounded border-gray-300 text-blue-600 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800"
                        @change="toggleTool(agent, tool.name, ($event.target as HTMLInputElement).checked)"
                      />
                      <span class="font-mono text-xs text-gray-700 dark:text-gray-300">{{ tool.name }}</span>
                    </label>
                  </div>
                </div>
              </div>

              <p v-if="hasToolRestriction(agent) && !toolGroupEntries.length" class="mt-2 text-xs text-gray-400 dark:text-gray-500">
                No tools found. The server may not be reachable.
              </p>
            </div>

            <div class="flex items-center justify-between">
              <h4 class="text-sm font-semibold text-gray-800 dark:text-gray-200">Channels</h4>
              <button type="button" class="rounded-lg border border-gray-200 px-3 py-1.5 text-xs text-gray-700 hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800" @click="addChannel(i)">+ Add Channel</button>
            </div>

            <div v-if="!agent.channels?.length" class="rounded-lg border border-dashed border-gray-300 px-3 py-2 text-xs text-gray-500 dark:border-gray-700 dark:text-gray-400">
              No channels configured for this agent.
            </div>

            <div v-for="(ch, k) in agent.channels" :key="`ch-${i}-${k}`" class="space-y-3 rounded-lg border border-gray-200 p-4 dark:border-gray-700">
              <div class="grid gap-3 lg:grid-cols-[160px_1fr_auto]">
                <div>
                  <label class="field-label">Type</label>
                  <select v-model="ch.type" class="field-input">
                    <option value="slack">slack</option>
                    <option value="discord">discord</option>
                    <option value="signal">signal</option>
                  </select>
                </div>
                <div class="flex items-end">
                  <button type="button" class="danger-btn" @click="removeChannel(i, k)">Remove</button>
                </div>
              </div>

              <!-- Allow From entries -->
              <div class="space-y-2">
                <div class="flex items-center justify-between">
                  <span class="field-label">Allow From</span>
                  <button type="button" class="rounded border border-gray-200 px-2 py-1 text-xs text-gray-700 hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800" @click="addAllowFrom(i, k)">+ Add Entry</button>
                </div>
                <div v-if="!ch.allowFrom?.length" class="rounded border border-dashed border-gray-300 px-3 py-2 text-xs text-gray-500 dark:border-gray-700 dark:text-gray-400">
                  No entries — all messages will be rejected.
                </div>
                <div v-for="(entry, ei) in ch.allowFrom" :key="`af-${i}-${k}-${ei}`" class="space-y-2 rounded border border-gray-100 p-3 dark:border-gray-800">
                  <div class="grid gap-2 lg:grid-cols-[1fr_auto]">
                    <div>
                      <label class="field-label">From (*, user ID, phone, group:*, group:&lt;id&gt; — comma-separated)</label>
                      <input v-model="entry.from" type="text" class="field-input" placeholder="*, group:*, +15551234567" />
                    </div>
                    <div class="flex items-end">
                      <button type="button" class="danger-btn" @click="removeAllowFrom(i, k, ei)">Remove</button>
                    </div>
                  </div>
                  <div class="grid gap-2 lg:grid-cols-2">
                    <div>
                      <label class="field-label">Mention Prefixes — group chats only (comma-separated glob patterns)</label>
                      <input :value="entryMentionPrefixes(entry)" type="text" class="field-input" placeholder="@bot*, !help" @input="setEntryMentionPrefixes(entry, $event)" />
                    </div>
                    <div class="flex items-end pb-1">
                      <label class="flex cursor-pointer items-center gap-2 text-xs text-gray-600 dark:text-gray-400">
                        <input type="checkbox" v-model="entry.respondToMentions" class="h-3.5 w-3.5 rounded border-gray-300 text-blue-600 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800" />
                        Respond to @mentions (group chats only)
                      </label>
                    </div>
                  </div>
                  <!-- Restrict Tools -->
                  <div>
                    <div class="flex items-center gap-3">
                      <label class="flex cursor-pointer items-center gap-2">
                        <input
                          type="checkbox"
                          :checked="hasEntryToolRestriction(entry)"
                          class="h-4 w-4 rounded border-gray-300 text-blue-600 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800"
                          @change="setEntryToolRestriction(entry, ($event.target as HTMLInputElement).checked)"
                        />
                        <span class="text-sm font-medium text-gray-700 dark:text-gray-300">Restrict tools</span>
                      </label>
                      <span class="text-xs text-gray-400 dark:text-gray-500">When checked, only the selected tools are available for this entry (overrides agent defaults).</span>
                    </div>
                    <div v-if="hasEntryToolRestriction(entry) && toolGroupEntries.length" class="mt-3 space-y-2">
                      <div
                        v-for="[cat, catTools] in toolGroupEntries"
                        :key="cat"
                        class="rounded-lg border border-gray-200 p-3 dark:border-gray-700"
                      >
                        <label class="mb-2 flex cursor-pointer items-center gap-2">
                          <input
                            type="checkbox"
                            :checked="isEntryCategoryFullyEnabled(entry, cat)"
                            :indeterminate="isEntryCategoryPartiallyEnabled(entry, cat)"
                            class="h-4 w-4 rounded border-gray-300 text-blue-600 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800"
                            @change="toggleEntryCategory(entry, cat, ($event.target as HTMLInputElement).checked)"
                          />
                          <span class="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
                            {{ toolCategoryLabel(cat) }}
                          </span>
                        </label>
                        <div class="flex flex-wrap gap-x-5 gap-y-1.5 pl-6">
                          <label
                            v-for="tool in catTools"
                            :key="tool.name"
                            class="flex cursor-pointer items-center gap-1.5"
                          >
                            <input
                              type="checkbox"
                              :checked="isEntryToolEnabled(entry, tool.name)"
                              class="h-3.5 w-3.5 rounded border-gray-300 text-blue-600 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800"
                              @change="toggleEntryTool(entry, tool.name, ($event.target as HTMLInputElement).checked)"
                            />
                            <span class="font-mono text-xs text-gray-700 dark:text-gray-300">{{ tool.name }}</span>
                          </label>
                        </div>
                      </div>
                    </div>
                    <p v-if="hasEntryToolRestriction(entry) && !toolGroupEntries.length" class="mt-2 text-xs text-gray-400 dark:text-gray-500">
                      No tools found. The server may not be reachable.
                    </p>
                  </div>
                </div>
              </div>

              <div v-if="ch.type === 'slack'" class="grid gap-3 lg:grid-cols-2">
                <div>
                  <label class="field-label">App-Level Token (xapp-…)</label>
                  <input v-model="ch.url" type="text" class="field-input" placeholder="xapp-..." />
                </div>
                <div>
                  <label class="field-label">Bot Token (xoxb-…)</label>
                  <input v-model="ch.token" type="text" class="field-input" placeholder="xoxb-..." />
                </div>
              </div>

              <div v-if="ch.type === 'discord'" class="grid gap-3 lg:grid-cols-1">
                <div>
                  <label class="field-label">Bot Token</label>
                  <input v-model="ch.token" type="text" class="field-input" placeholder="Discord bot token" />
                </div>
              </div>

              <div v-if="ch.type === 'signal'" class="grid gap-3 lg:grid-cols-2">
                <div>
                  <label class="field-label">Account Phone (E.164)</label>
                  <input v-model="ch.phone" type="text" class="field-input" placeholder="+15551234567" />
                </div>
                <div>
                  <label class="field-label">signal-cli Daemon Address</label>
                  <input v-model="ch.url" type="text" class="field-input" placeholder="127.0.0.1:7583" />
                </div>
              </div>
            </div>

            <div class="flex items-center justify-between">
              <h4 class="text-sm font-semibold text-gray-800 dark:text-gray-200">Tasks</h4>
              <button type="button" class="rounded-lg border border-gray-200 px-3 py-1.5 text-xs text-gray-700 hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800" @click="addTask(i)">+ Add Task</button>
            </div>

            <div v-if="!agent.tasks?.length" class="rounded-lg border border-dashed border-gray-300 px-3 py-2 text-xs text-gray-500 dark:border-gray-700 dark:text-gray-400">
              No tasks configured for this agent.
            </div>

            <div v-for="(task, j) in agent.tasks" :key="`task-${i}-${j}`" class="space-y-3 rounded-lg border border-gray-200 p-4 dark:border-gray-700">
              <div class="grid gap-3 lg:grid-cols-[1fr_1fr_1fr_1fr_auto]">
                <div>
                  <label class="field-label">Task name</label>
                  <input v-model="task.name" type="text" class="field-input" placeholder="daily-briefing" />
                </div>
                <div>
                  <label class="field-label">Schedule</label>
                  <input v-model="task.schedule" type="text" class="field-input" placeholder="0 * * * * *" />
                </div>
                <div>
                  <label class="field-label">Watch</label>
                  <input v-model="task.watch" type="text" class="field-input" placeholder="./docs/**/*.md" />
                </div>
                <div>
                  <label class="field-label">Channel</label>
                  <select v-model="task.channel" class="field-input">
                    <option value="">silent</option>
                    <option value="last">last</option>
                    <option value="slack">slack</option>
                    <option value="discord">discord</option>
                    <option value="signal">signal</option>
                  </select>
                </div>
                <div class="flex items-end">
                  <button type="button" class="danger-btn" @click="removeTask(i, j)">Remove Task</button>
                </div>
              </div>

              <div class="grid gap-3 lg:grid-cols-[1fr_auto]">
                <div>
                  <label class="field-label">Prompt</label>
                  <textarea v-model="task.prompt" rows="3" class="field-input" placeholder="Task prompt..."></textarea>
                </div>
                <label class="mt-6 flex items-center gap-2 text-xs text-gray-600 dark:text-gray-400">
                  <input v-model="task.run_once" type="checkbox" class="accent-blue-600" />
                  Run once
                </label>
              </div>
            </div>

            <div>
              <div class="mb-2 flex items-center justify-between">
                <h4 class="text-sm font-semibold text-gray-800 dark:text-gray-200">Queued & Recent Jobs</h4>
                <button type="button" class="rounded-lg border border-gray-200 px-3 py-1 text-xs text-gray-700 hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800" :disabled="jobsLoading" @click="loadAllJobs">{{ jobsLoading ? 'Loading…' : 'Refresh' }}</button>
              </div>
              <div v-if="!agentJobsList(agent.name).length" class="rounded-lg border border-dashed border-gray-300 px-3 py-2 text-xs text-gray-500 dark:border-gray-700 dark:text-gray-400">
                No queued or recent jobs.
              </div>
              <div v-else class="overflow-hidden rounded-lg border border-gray-200 dark:border-gray-700">
                <table class="w-full text-xs">
                  <thead>
                    <tr class="border-b border-gray-200 text-left font-medium text-gray-500 dark:border-gray-700 dark:text-gray-400">
                      <th class="px-3 py-2">Task</th>
                      <th class="px-3 py-2">Status</th>
                      <th class="px-3 py-2">When</th>
                      <th class="px-3 py-2">Prompt</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="job in agentJobsList(agent.name)" :key="job.id" class="border-b border-gray-100 text-gray-700 last:border-0 dark:border-gray-800 dark:text-gray-300">
                      <td class="px-3 py-2 font-mono">{{ job.task_id }}</td>
                      <td class="px-3 py-2"><span :class="jobStatusClass(job.status)" class="rounded px-1.5 py-0.5 text-xs font-medium">{{ job.status }}</span></td>
                      <td class="px-3 py-2 text-gray-500 dark:text-gray-400">{{ fmtJobDate(job.scheduled_for ?? job.created_at) }}</td>
                      <td class="max-w-xs truncate px-3 py-2 text-gray-500 dark:text-gray-400">{{ job.prompt }}</td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </div>
          </div>
        </section>

        <section v-show="activeTab === 'sessions'" class="space-y-5 pb-8">
          <div class="rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
            <h3 class="mb-4 text-sm font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Sessions</h3>
            <div class="grid gap-3 lg:grid-cols-[280px_auto_auto]">
              <div>
                <label class="field-label">Agent</label>
                <select v-model="sessionAgent" class="field-input">
                  <option value="">Select agent</option>
                  <option v-for="agent in draft.agents" :key="`sess-${agent.name}`" :value="agent.name">{{ agent.name }}</option>
                </select>
              </div>
              <div class="flex items-end">
                <button type="button" class="rounded-lg border border-gray-200 px-3 py-2 text-xs text-gray-700 hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800" :disabled="!sessionAgent || sessionLoading" @click="loadSessions">{{ sessionLoading ? 'Loading…' : 'Refresh Sessions' }}</button>
              </div>
              <div class="flex items-end">
                <button type="button" class="rounded-lg bg-blue-600 px-3 py-2 text-xs font-semibold text-white hover:bg-blue-500 disabled:opacity-50" :disabled="!sessionAgent || sessionLoading" @click="createSession">+ Create Session</button>
              </div>
            </div>

            <div class="mt-4 overflow-hidden rounded-lg border border-gray-200 dark:border-gray-700">
              <table v-if="sessions.length" class="w-full text-sm">
                <thead>
                  <tr class="border-b border-gray-200 text-left text-xs font-medium text-gray-500 dark:border-gray-700 dark:text-gray-400">
                    <th class="px-3 py-2">Name</th>
                    <th class="px-3 py-2">ID</th>
                    <th class="px-3 py-2">Updated</th>
                    <th class="px-3 py-2">Action</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="s in sessions" :key="s.id" class="border-b border-gray-100 text-gray-700 dark:border-gray-800 dark:text-gray-300">
                    <td class="px-3 py-2">{{ s.name || '—' }}</td>
                    <td class="px-3 py-2 font-mono text-xs text-gray-500 dark:text-gray-400">{{ s.id.slice(-10) }}</td>
                    <td class="px-3 py-2 text-xs">{{ formatDate(s.updated_at) }}</td>
                    <td class="px-3 py-2">
                      <button type="button" class="danger-btn" @click="stopSession(s.id)">Stop</button>
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
            <h3 class="mb-2 text-sm font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Credentials</h3>
            <p class="mb-4 text-xs text-gray-500 dark:text-gray-400">Configure authentication for LLM providers. OAuth tokens are stored securely and refreshed automatically.</p>

            <!-- Existing provider credentials -->
            <div v-if="configuredProviders.length" class="mb-4 overflow-hidden rounded-lg border border-gray-200 dark:border-gray-700">
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
                  <tr v-for="entry in configuredProviders" :key="entry.key" class="border-b border-gray-100 last:border-0 dark:border-gray-800">
                    <td class="px-3 py-2 font-medium text-gray-800 dark:text-gray-200">{{ entry.providerLabel }}</td>
                    <td class="px-3 py-2">
                      <span :class="entry.authType === 'oauth' ? 'inline-block rounded bg-blue-100 px-1.5 py-0.5 text-xs font-medium text-blue-700 dark:bg-blue-900/30 dark:text-blue-300' : 'inline-block rounded bg-gray-100 px-1.5 py-0.5 text-xs font-medium text-gray-600 dark:bg-gray-800 dark:text-gray-300'">
                        {{ entry.authType === 'oauth' ? 'OAuth' : 'API Key' }}
                      </span>
                    </td>
                    <td class="px-3 py-2">
                      <span v-if="entry.authType === 'oauth'" class="inline-flex items-center gap-1 text-green-600 dark:text-green-400">
                        <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5" viewBox="0 0 20 20" fill="currentColor"><path fill-rule="evenodd" d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z" clip-rule="evenodd" /></svg>
                        Authorized
                      </span>
                      <span v-else class="tracking-widest text-gray-400 dark:text-gray-500">••••••••</span>
                    </td>
                    <td class="px-3 py-2 text-right">
                      <div class="flex items-center justify-end gap-2">
                        <button v-if="entry.authType === 'oauth'" type="button" class="text-xs text-blue-600 hover:underline disabled:opacity-50 dark:text-blue-400" :disabled="oauthBusy" @click="reauthorizeProvider(entry.provider)">Re-authorize</button>
                        <button type="button" class="text-gray-400 hover:text-red-500 dark:text-gray-500 dark:hover:text-red-400" :title="`Remove ${entry.key}`" @click="deleteProviderCredential(entry.key)">
                          <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5" viewBox="0 0 20 20" fill="currentColor"><path fill-rule="evenodd" d="M9 2a1 1 0 00-.894.553L7.382 4H4a1 1 0 000 2v10a2 2 0 002 2h8a2 2 0 002-2V6a1 1 0 100-2h-3.382l-.724-1.447A1 1 0 0011 2H9zM7 8a1 1 0 012 0v6a1 1 0 11-2 0V8zm5-1a1 1 0 00-1 1v6a1 1 0 102 0V8a1 1 0 00-1-1z" clip-rule="evenodd" /></svg>
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
                  <input v-model="providerApiKeyValue" type="password" class="field-input max-w-[260px]" placeholder="API key…" />
                  <button type="button" class="rounded-lg bg-blue-600 px-3 py-2 text-xs font-semibold text-white hover:bg-blue-500 disabled:opacity-50" :disabled="!providerApiKeyValue.trim()" @click="addProviderApiKey">Add</button>
                </template>
                <button v-else type="button" class="rounded-lg bg-blue-600 px-3 py-2 text-xs font-semibold text-white hover:bg-blue-500 disabled:opacity-50" :disabled="oauthBusy" @click="addProviderOAuth">
                  {{ oauthBusy ? 'Authorizing…' : 'Authorize' }}
                </button>
              </template>
            </div>
            <p v-else-if="!configuredProviders.length" class="text-xs text-gray-400 dark:text-gray-500">No providers configured yet. Use the dropdown above to add one.</p>

            <!-- Anthropic two-step OAuth inline form -->
            <div v-if="anthropicUrl" class="mt-3 space-y-2 rounded-lg border border-gray-200 p-3 dark:border-gray-700">
              <p class="text-xs text-gray-500 dark:text-gray-400">Open the link below, sign in, and paste the code shown:</p>
              <a :href="anthropicUrl" target="_blank" rel="noreferrer" class="block truncate text-xs text-blue-600 hover:text-blue-500 dark:text-blue-400">{{ anthropicUrl }}</a>
              <div class="flex gap-2">
                <input v-model="anthropicCode" type="text" class="field-input" placeholder="Paste code here…" />
                <button type="button" class="rounded-lg bg-blue-600 px-3 py-2 text-xs font-semibold text-white hover:bg-blue-500 disabled:opacity-50" :disabled="oauthBusy || !anthropicCode.trim()" @click="completeAnthropic">Complete</button>
              </div>
            </div>
          </div>

          <!-- Extra Secrets -->
          <div class="rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
            <h3 class="mb-2 text-sm font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Extra Secrets</h3>
            <p class="mb-4 text-xs text-gray-500 dark:text-gray-400">Store arbitrary secrets for use by tools and agents (e.g. a Brave API key or Twilio auth token).</p>
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
                      <input v-model="secretName" type="text" class="field-input py-1.5 font-mono text-xs" placeholder="brave_api_key" />
                    </td>
                    <td class="px-2 py-1.5">
                      <input v-model="secretValue" type="password" class="field-input py-1.5 text-xs" placeholder="…" />
                    </td>
                    <td class="px-2 py-1.5">
                      <button type="button" class="rounded-lg bg-blue-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-blue-500" @click="addSecret">Add</button>
                    </td>
                  </tr>
                  <tr v-for="name in extraSecrets" :key="name" class="border-b border-gray-100 last:border-0 dark:border-gray-800">
                    <td class="px-3 py-2 font-mono text-gray-700 dark:text-gray-300">{{ name }}</td>
                    <td class="px-3 py-2 tracking-widest text-gray-400 dark:text-gray-500">••••••••</td>
                    <td class="px-3 py-2 text-right">
                      <button type="button" class="text-gray-400 hover:text-red-500 dark:text-gray-500 dark:hover:text-red-400" :title="`Delete ${name}`" @click="deleteSecret(name)">
                        <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5" viewBox="0 0 20 20" fill="currentColor"><path fill-rule="evenodd" d="M9 2a1 1 0 00-.894.553L7.382 4H4a1 1 0 000 2v10a2 2 0 002 2h8a2 2 0 002-2V6a1 1 0 100-2h-3.382l-.724-1.447A1 1 0 0011 2H9zM7 8a1 1 0 012 0v6a1 1 0 11-2 0V8zm5-1a1 1 0 00-1 1v6a1 1 0 102 0V8a1 1 0 00-1-1z" clip-rule="evenodd" /></svg>
                      </button>
                    </td>
                  </tr>
                  <tr v-if="!extraSecrets.length">
                    <td colspan="3" class="px-3 py-3 text-center text-gray-400 dark:text-gray-500">No extra secrets stored yet.</td>
                  </tr>
                </tbody>
              </table>
            </div>
            <button type="button" class="mt-2 text-xs text-gray-400 hover:text-gray-600 dark:text-gray-500 dark:hover:text-gray-300" @click="refreshCredentials">↻ Refresh</button>
          </div>
        </section>

        <section v-show="activeTab === 'memory'" class="space-y-5 pb-8">
          <div class="rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
            <h3 class="mb-2 text-sm font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Agent Memory</h3>
            <p class="mb-4 text-xs text-gray-500 dark:text-gray-400">Notes the agent has remembered. Edit freely — changes are saved as the agent's memory file.</p>

            <div class="mb-4 flex gap-3">
              <select v-model="memoryAgent" class="field-input max-w-[220px]">
                <option value="">Select agent</option>
                <option v-for="agent in draft.agents" :key="`mem-${agent.name}`" :value="agent.name">{{ agent.name }}</option>
              </select>
              <button type="button" class="rounded-lg bg-blue-600 px-3 py-2 text-xs font-semibold text-white hover:bg-blue-500 disabled:opacity-50" :disabled="!memoryAgent || notesSaving" @click="saveNotes">{{ notesSaving ? 'Saving…' : 'Save' }}</button>
              <button type="button" class="danger-btn disabled:opacity-50" :disabled="!memoryAgent || memoryClearing" @click="clearMemory">{{ memoryClearing ? 'Clearing…' : 'Clear All' }}</button>
            </div>

            <div v-if="memoryErrorMessage" class="mb-3 rounded-lg bg-red-50 px-3 py-2 text-xs text-red-700 dark:bg-red-950 dark:text-red-300">{{ memoryErrorMessage }}</div>
            <div v-if="memoryLoading" class="text-xs text-gray-500 dark:text-gray-400">Loading…</div>
            <template v-else>
              <textarea
                v-model="notesContent"
                class="field-input min-h-[280px] resize-y font-mono text-xs"
                :placeholder="memoryAgent ? 'No notes yet. The agent writes here via memory_store, or you can type directly.' : 'Select an agent to view and edit its notes.'"
                :disabled="!memoryAgent"
              />
            </template>
          </div>
        </section>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import AppLayout from "../components/AppLayout.vue";
import ModelSelector from "../components/ModelSelector.vue";
import { type MCPToolInfo, useMCP } from "../composables/useMCP";
import { SUPPORTED_MODELS } from "../constants/models";
import { useAuthStore } from "../stores/auth";
import {
	type AgentChannel,
	type AgentEntry,
	type AgentTask,
	type AllowFromEntry,
	type AppConfig,
	useSettingsStore,
} from "../stores/settings";

type Tab = "general" | "agents" | "sessions" | "providers" | "memory";

interface SessionRow {
	id: string;
	name: string;
	updated_at: string;
}

interface RuntimeAgent {
	name: string;
	model?: string;
	fallbacks?: string[];
}

interface JobEntry {
	id: string;
	task_id: string;
	agent_name: string;
	status: string;
	prompt: string;
	scheduled_for?: string;
	created_at: string;
}

const route = useRoute();
const router = useRouter();

const tabs: Tab[] = ["general", "agents", "sessions", "providers", "memory"];
const queryTab = route.query.tab as Tab | undefined;
const storedTab = localStorage.getItem("settings:activeTab") as Tab | null;
const activeTab = ref<Tab>(
	queryTab && tabs.includes(queryTab)
		? queryTab
		: storedTab && tabs.includes(storedTab)
			? storedTab
			: "general",
);

watch(activeTab, (tab) => {
	localStorage.setItem("settings:activeTab", tab);
	void router.replace({ query: { ...route.query, tab } });
});

const store = useSettingsStore();
const { callTool, listTools } = useMCP();
const authStore = useAuthStore();

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
			};
			if (
				data.type === "session_message" ||
				data.type === "session_processing"
			) {
				if (activeTab.value === "sessions" && sessionAgent.value) {
					await loadSessions();
				}
				if (activeTab.value === "agents") {
					void loadAllJobs();
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
const errorMessage = ref("");
const okMessage = ref("");

const draft = ref<AppConfig>(emptyConfig());

const concurrencyInput = ref("");

const sessionAgent = ref("");
const sessions = ref<SessionRow[]>([]);
const sessionLoading = ref(false);

const credentials = ref<string[]>([]);
const oauthBusy = ref(false);
const anthropicUrl = ref("");
const anthropicCode = ref("");
const providerAddSelection = ref("");
const providerApiKeyValue = ref("");
const secretName = ref("");
const secretValue = ref("");

const KNOWN_PROVIDERS = [
	{ id: "anthropic", label: "Anthropic" },
	{ id: "openai", label: "OpenAI" },
	{ id: "google", label: "Google" },
] as const;

const configuredProviders = computed(() => {
	const entries: Array<{
		key: string;
		provider: string;
		providerLabel: string;
		authType: "oauth" | "apikey";
	}> = [];
	for (const cred of credentials.value) {
		for (const p of KNOWN_PROVIDERS) {
			const authId = p.id === "google" ? "gemini" : p.id;
			if (cred === `${authId}:oauth`) {
				entries.push({
					key: cred,
					provider: p.id,
					providerLabel: p.label,
					authType: "oauth",
				});
			} else if (cred === `${authId}:default`) {
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
		const authId = p.id === "google" ? "gemini" : p.id;
		if (!configured.has(`${authId}:oauth`)) {
			options.push({
				key: `${authId}:oauth`,
				label: `${p.label} (OAuth)`,
				provider: p.id,
			});
		}
		if (!configured.has(`${authId}:default`)) {
			options.push({
				key: `${authId}:apikey`,
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
		const authId = p.id === "google" ? "gemini" : p.id;
		providerKeys.add(`${authId}:oauth`);
		providerKeys.add(`${authId}:default`);
	}
	return credentials.value.filter((cred) => !providerKeys.has(cred));
});

const allJobs = ref<JobEntry[]>([]);
const jobsLoading = ref(false);

const memoryAgent = ref("");
const notesContent = ref("");
const memoryLoading = ref(false);
const memoryClearing = ref(false);
const notesSaving = ref(false);

const availableTools = ref<MCPToolInfo[]>([]);

function toolCategory(name: string): string {
	if (
		name === "ping" ||
		name.startsWith("server_") ||
		name.startsWith("config_")
	)
		return "server";
	if (name.startsWith("web_")) return "search";
	return name.split("_")[0] ?? name;
}

function toolCategoryLabel(cat: string): string {
	const labels: Record<string, string> = {
		agent: "Agent",
		session: "Sessions",
		task: "Tasks",
		job: "Jobs",
		browser: "Browser",
		search: "Search",
		memory: "Memory",
		auth: "Auth",
		server: "Server",
		usage: "Usage",
	};
	return labels[cat] ?? cat.charAt(0).toUpperCase() + cat.slice(1);
}

const toolGroupEntries = computed((): [string, MCPToolInfo[]][] => {
	const groups = new Map<string, MCPToolInfo[]>();
	for (const tool of availableTools.value) {
		const cat = toolCategory(tool.name);
		const list = groups.get(cat) ?? [];
		list.push(tool);
		groups.set(cat, list);
	}
	return [...groups.entries()];
});
const memoryErrorMessage = ref("");

watch(memoryAgent, (agent) => {
	notesContent.value = "";
	memoryErrorMessage.value = "";
	if (agent && activeTab.value === "memory") void loadNotes();
});

watch(activeTab, (tab) => {
	if (tab === "memory" && memoryAgent.value && !memoryLoading.value) {
		void loadNotes();
	}
	if (tab === "agents") {
		void loadAllJobs();
	}
	if (tab === "sessions" && sessionAgent.value) {
		void loadSessions();
	}
});

interface RulesEditorState {
	content: string;
	loading: boolean;
	saving: boolean;
	error: string;
}
const rulesEditorState = ref<Record<string, RulesEditorState>>({});

function getRulesState(agentName: string): RulesEditorState {
	if (!rulesEditorState.value[agentName]) {
		rulesEditorState.value[agentName] = {
			content: "",
			loading: false,
			saving: false,
			error: "",
		};
	}
	return rulesEditorState.value[agentName];
}

async function loadRulesFile(agentName: string) {
	if (!agentName) return;
	const state = getRulesState(agentName);
	state.loading = true;
	state.error = "";
	try {
		state.content = await callTool("agent_rules_get", { name: agentName });
	} catch (e) {
		state.error = e instanceof Error ? e.message : String(e);
	} finally {
		state.loading = false;
	}
}

async function saveRulesFile(agentName: string) {
	if (!agentName) return;
	const state = getRulesState(agentName);
	state.saving = true;
	state.error = "";
	try {
		await callTool("agent_rules_set", {
			agent: agentName,
			content: state.content,
		});
	} catch (e) {
		state.error = e instanceof Error ? e.message : String(e);
	} finally {
		state.saving = false;
	}
}

function agentJobsList(agentName: string): JobEntry[] {
	return allJobs.value.filter((j) => j.agent_name === agentName);
}

async function loadAllJobs() {
	if (jobsLoading.value) return;
	jobsLoading.value = true;
	try {
		const raw = await callTool("job_list", {});
		allJobs.value = (JSON.parse(raw) as JobEntry[] | null) ?? [];
	} catch {
		allJobs.value = [];
	} finally {
		jobsLoading.value = false;
	}
}

function jobStatusClass(status: string): string {
	if (status === "done")
		return "bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400";
	if (status === "failed")
		return "bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400";
	if (status === "in_progress")
		return "bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400";
	return "bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400";
}

function fmtJobDate(s: string | undefined): string {
	if (!s) return "—";
	return new Date(s).toLocaleString();
}

onMounted(async () => {
	connectWs();
	await loadConfig();
	await refreshCredentials();
	void loadAllJobs();
});

onUnmounted(() => {
	settingsWs?.close();
	settingsWs = null;
});

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
		browser: { binary: "", cdp_port: 0 },
		scheduler: { concurrency: "" },
	};
}

function tabLabel(tab: Tab): string {
	if (tab === "general") return "General";
	if (tab === "agents") return "Agents & Tasks";
	if (tab === "sessions") return "Sessions";
	if (tab === "memory") return "Memory";
	return "Providers & Auth";
}

function tabClass(tab: Tab): string {
	return activeTab.value === tab
		? "rounded-lg bg-gray-900 px-3 py-1.5 text-xs font-semibold text-white dark:bg-white dark:text-gray-900"
		: "rounded-lg border border-gray-200 px-3 py-1.5 text-xs font-medium text-gray-600 hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800";
}

async function loadConfig() {
	loading.value = true;
	errorMessage.value = "";
	okMessage.value = "";
	try {
		await store.fetchConfig();
		const cfg = store.config
			? (JSON.parse(JSON.stringify(store.config)) as AppConfig)
			: emptyConfig();
		draft.value = cfg;
		concurrencyInput.value = cfg.scheduler.concurrency
			? String(cfg.scheduler.concurrency)
			: "";

		if (!draft.value.agents.length) {
			await importAgents();
		}

		if (!sessionAgent.value && draft.value.agents.length) {
			sessionAgent.value = draft.value.agents[0].name;
		}
		if (!memoryAgent.value && draft.value.agents.length) {
			memoryAgent.value = draft.value.agents[0].name;
		}

		// Fetch the available tool list once so the permissions UI can render.
		if (!availableTools.value.length) {
			availableTools.value = await listTools().catch(() => []);
		}
	} catch (e) {
		errorMessage.value = e instanceof Error ? e.message : String(e);
	} finally {
		loading.value = false;
	}
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
}

function removeAgent(index: number) {
	draft.value.agents.splice(index, 1);
}

function addTask(agentIndex: number) {
	const task: AgentTask = {
		name: "",
		prompt: "",
		schedule: "",
		watch: "",
		channel: "",
		run_once: false,
	};
	if (!Array.isArray(draft.value.agents[agentIndex].tasks)) {
		draft.value.agents[agentIndex].tasks = [];
	}
	draft.value.agents[agentIndex].tasks.push(task);
}

function removeTask(agentIndex: number, taskIndex: number) {
	draft.value.agents[agentIndex].tasks.splice(taskIndex, 1);
}

function addChannel(agentIndex: number) {
	const ch: AgentChannel = { type: "signal" };
	if (!Array.isArray(draft.value.agents[agentIndex].channels)) {
		draft.value.agents[agentIndex].channels = [];
	}
	draft.value.agents[agentIndex].channels.push(ch);
}

function removeChannel(agentIndex: number, chIndex: number) {
	draft.value.agents[agentIndex].channels.splice(chIndex, 1);
}

function addAllowFrom(agentIndex: number, chIndex: number) {
	const ch = draft.value.agents[agentIndex].channels[chIndex];
	if (!Array.isArray(ch.allowFrom)) {
		ch.allowFrom = [];
	}
	ch.allowFrom.push({ from: "", respondToMentions: true });
}

function removeAllowFrom(
	agentIndex: number,
	chIndex: number,
	entryIndex: number,
) {
	draft.value.agents[agentIndex].channels[chIndex].allowFrom?.splice(
		entryIndex,
		1,
	);
}

function entryMentionPrefixes(entry: AllowFromEntry): string {
	return (entry.mentionPrefixes ?? []).join(", ");
}

function setEntryMentionPrefixes(entry: AllowFromEntry, event: Event) {
	entry.mentionPrefixes = (event.target as HTMLInputElement).value
		.split(",")
		.map((v) => v.trim())
		.filter(Boolean);
}

function hasEntryToolRestriction(entry: AllowFromEntry): boolean {
	return (entry.restrictTools?.length ?? 0) > 0;
}

function setEntryToolRestriction(entry: AllowFromEntry, restricted: boolean) {
	if (restricted) {
		entry.restrictTools = availableTools.value.map((t) => t.name);
	} else {
		entry.restrictTools = undefined;
	}
}

function isEntryToolEnabled(entry: AllowFromEntry, toolName: string): boolean {
	if (!hasEntryToolRestriction(entry)) return true;
	return entry.restrictTools?.includes(toolName) ?? false;
}

function toggleEntryTool(
	entry: AllowFromEntry,
	toolName: string,
	enabled: boolean,
) {
	if (!entry.restrictTools) entry.restrictTools = [];
	const idx = entry.restrictTools.indexOf(toolName);
	if (enabled && idx === -1) {
		entry.restrictTools.push(toolName);
	} else if (!enabled && idx !== -1) {
		entry.restrictTools.splice(idx, 1);
	}
}

function isEntryCategoryFullyEnabled(
	entry: AllowFromEntry,
	cat: string,
): boolean {
	const catTools = availableTools.value.filter(
		(t) => toolCategory(t.name) === cat,
	);
	return catTools.every((t) => isEntryToolEnabled(entry, t.name));
}

function isEntryCategoryPartiallyEnabled(
	entry: AllowFromEntry,
	cat: string,
): boolean {
	const catTools = availableTools.value.filter(
		(t) => toolCategory(t.name) === cat,
	);
	const enabledCount = catTools.filter((t) =>
		isEntryToolEnabled(entry, t.name),
	).length;
	return enabledCount > 0 && enabledCount < catTools.length;
}

function toggleEntryCategory(
	entry: AllowFromEntry,
	cat: string,
	enabled: boolean,
) {
	const catTools = availableTools.value.filter(
		(t) => toolCategory(t.name) === cat,
	);
	for (const t of catTools) {
		toggleEntryTool(entry, t.name, enabled);
	}
}

function hasToolRestriction(agent: AgentEntry): boolean {
	return (agent.permissions?.tools?.length ?? 0) > 0;
}

function setToolRestriction(agent: AgentEntry, restricted: boolean) {
	if (restricted) {
		// Start with all tools selected so nothing breaks immediately.
		agent.permissions = { tools: availableTools.value.map((t) => t.name) };
	} else {
		agent.permissions = undefined;
	}
}

function isToolEnabled(agent: AgentEntry, toolName: string): boolean {
	if (!hasToolRestriction(agent)) return true;
	return agent.permissions?.tools?.includes(toolName) ?? false;
}

function toggleTool(agent: AgentEntry, toolName: string, enabled: boolean) {
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
	return catTools.every((t) => isToolEnabled(agent, t.name));
}

function isCategoryPartiallyEnabled(agent: AgentEntry, cat: string): boolean {
	const catTools = availableTools.value.filter(
		(t) => toolCategory(t.name) === cat,
	);
	const enabledCount = catTools.filter((t) =>
		isToolEnabled(agent, t.name),
	).length;
	return enabledCount > 0 && enabledCount < catTools.length;
}

function toggleCategory(agent: AgentEntry, cat: string, enabled: boolean) {
	const catTools = availableTools.value.filter(
		(t) => toolCategory(t.name) === cat,
	);
	for (const t of catTools) {
		toggleTool(agent, t.name, enabled);
	}
}

async function importAgents() {
	try {
		const raw = await callTool("agent_list");
		const agents = (JSON.parse(raw) as RuntimeAgent[] | null) ?? [];
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
		if (!memoryAgent.value && draft.value.agents.length) {
			memoryAgent.value = draft.value.agents[0].name;
		}
	} catch {
		// best-effort import
	}
}

async function saveAll() {
	saving.value = true;
	errorMessage.value = "";
	okMessage.value = "";
	try {
		const normalized = JSON.parse(JSON.stringify(draft.value)) as AppConfig;

		const conc = concurrencyInput.value.trim();
		if (!conc || conc.toLowerCase() === "auto") {
			normalized.scheduler.concurrency = ""; // backend normalize() strips empty/"auto"
		} else {
			const n = Number.parseInt(conc, 10);
			normalized.scheduler.concurrency = Number.isNaN(n) || n < 1 ? "" : n;
		}

		// Normalize agent/task values.
		normalized.agents = (normalized.agents ?? []).map((agent) => ({
			...agent,
			name: (agent.name ?? "").trim(),
			model: (agent.model ?? "").trim(),
			memory: (agent.memory ?? "").trim(),
			rules: (agent.rules ?? "").trim() || undefined,
			fallbacks: (agent.fallbacks ?? []).map((v) => v.trim()).filter(Boolean),
			channels: (agent.channels ?? []).map((ch) => ({
				...ch,
				type: (ch.type ?? "").trim(),
				token: (ch.token ?? "").trim() || undefined,
				channel: (ch.channel ?? "").trim() || undefined,
				phone: (ch.phone ?? "").trim() || undefined,
				url: (ch.url ?? "").trim() || undefined,
				allowFrom: (ch.allowFrom ?? [])
					.map((entry) => ({
						...entry,
						from: (entry.from ?? "").trim(),
						mentionPrefixes: (entry.mentionPrefixes ?? [])
							.map((v) => v.trim())
							.filter(Boolean),
						restrictTools: (entry.restrictTools ?? [])
							.map((v) => v.trim())
							.filter(Boolean),
					}))
					.filter((entry) => entry.from),
			})),
			tasks: (agent.tasks ?? []).map((task) => ({
				...task,
				name: (task.name ?? "").trim(),
				prompt: (task.prompt ?? "").trim(),
				schedule: (task.schedule ?? "").trim(),
				watch: (task.watch ?? "").trim(),
				start_at: (task.start_at ?? "").trim(),
				channel: (task.channel ?? "").trim(),
				run_once: Boolean(task.run_once),
			})),
			permissions:
				(agent.permissions?.tools?.length ?? 0) > 0
					? { tools: (agent.permissions?.tools ?? []).filter(Boolean) }
					: undefined,
		}));

		await store.saveConfig(normalized);
		draft.value = JSON.parse(JSON.stringify(normalized)) as AppConfig;
		okMessage.value = "Settings saved.";
	} catch (e) {
		errorMessage.value = e instanceof Error ? e.message : String(e);
	} finally {
		saving.value = false;
	}
}

async function loadSessions() {
	if (!sessionAgent.value) return;
	sessionLoading.value = true;
	errorMessage.value = "";
	try {
		const raw = await callTool("session_list", { agent: sessionAgent.value });
		sessions.value = (JSON.parse(raw) as SessionRow[] | null) ?? [];
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
		await callTool("session_stop", { session_id: sessionID });
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

async function refreshCredentials() {
	try {
		const raw = await callTool("auth_list");
		credentials.value = (JSON.parse(raw) as string[] | null) ?? [];
	} catch {
		credentials.value = [];
	}
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
	const provider = providerAddSelection.value.replace(/:oauth$/, "");
	if (provider === "anthropic") {
		await startAnthropic();
	} else if (provider === "openai") {
		await loginOpenAI();
		providerAddSelection.value = "";
	} else if (provider === "google") {
		await loginGemini();
		providerAddSelection.value = "";
	}
}

async function reauthorizeProvider(provider: string) {
	if (provider === "anthropic") {
		await startAnthropic();
	} else if (provider === "openai") {
		await loginOpenAI();
	} else if (provider === "google") {
		await loginGemini();
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

async function loadNotes() {
	if (!memoryAgent.value) return;
	memoryLoading.value = true;
	memoryErrorMessage.value = "";
	try {
		notesContent.value = await callTool("memory_show", {
			agent: memoryAgent.value,
		});
	} catch (e) {
		memoryErrorMessage.value = e instanceof Error ? e.message : String(e);
	} finally {
		memoryLoading.value = false;
	}
}

async function saveNotes() {
	if (!memoryAgent.value) return;
	notesSaving.value = true;
	memoryErrorMessage.value = "";
	try {
		await callTool("memory_notes_set", {
			agent: memoryAgent.value,
			content: notesContent.value,
		});
	} catch (e) {
		memoryErrorMessage.value = e instanceof Error ? e.message : String(e);
	} finally {
		notesSaving.value = false;
	}
}

async function clearMemory() {
	if (!memoryAgent.value) return;
	memoryClearing.value = true;
	memoryErrorMessage.value = "";
	try {
		await callTool("memory_clear", { agent: memoryAgent.value });
		notesContent.value = "";
		okMessage.value = `Memory cleared for agent "${memoryAgent.value}".`;
	} catch (e) {
		memoryErrorMessage.value = e instanceof Error ? e.message : String(e);
	} finally {
		memoryClearing.value = false;
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
</style>
