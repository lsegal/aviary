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
                <input v-model.number="draft.server.port" type="number" min="1" max="65535" class="field-input" />
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
          </div>

          <div class="rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
            <h3 class="mb-4 text-sm font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Models</h3>
            <div class="grid gap-4 lg:grid-cols-2">
              <div>
                <label class="field-label">Default model</label>
                <input v-model="draft.models.defaults.model" type="text" class="field-input" placeholder="anthropic/claude-sonnet-4-5" />
              </div>
              <div>
                <label class="field-label">Default fallbacks (comma-separated)</label>
                <input v-model="fallbacksCsv" type="text" class="field-input" placeholder="openai/gpt-4o-mini, gemini/gemini-pro" />
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
                <input v-model.number="draft.browser.cdp_port" type="number" min="1" max="65535" class="field-input" />
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
            <div class="grid gap-4 lg:grid-cols-[1fr_1fr_1fr_auto]">
              <div>
                <label class="field-label">Name</label>
                <input v-model="agent.name" type="text" class="field-input" placeholder="assistant" />
              </div>
              <div>
                <label class="field-label">Model</label>
                <input v-model="agent.model" type="text" class="field-input" placeholder="anthropic/claude-sonnet-4-5" />
              </div>
              <div>
                <label class="field-label">Fallbacks (comma-separated)</label>
                <input :value="agentFallbacks(agent)" type="text" class="field-input" placeholder="openai/gpt-4o-mini" @input="setAgentFallbacks(agent, $event)" />
              </div>
              <div class="flex items-end">
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
          <div class="rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
            <h3 class="mb-2 text-sm font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Providers mapping</h3>
            <p class="mb-4 text-xs text-gray-500 dark:text-gray-400">Provider name maps to an auth reference used by models.</p>
            <div class="space-y-2">
              <div v-for="(entry, i) in providerRows" :key="`provider-${i}`" class="grid gap-2 lg:grid-cols-[200px_1fr_auto]">
                <input v-model="entry.name" type="text" class="field-input" placeholder="openai" />
                <input v-model="entry.auth" type="text" class="field-input" placeholder="auth:openai:default" />
                <button type="button" class="danger-btn" @click="providerRows.splice(i, 1)">Remove</button>
              </div>
              <button type="button" class="rounded-lg border border-gray-200 px-3 py-1.5 text-xs text-gray-700 hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800" @click="providerRows.push({ name: '', auth: '' })">+ Add provider mapping</button>
            </div>
          </div>

          <div class="rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
            <h3 class="mb-2 text-sm font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Credentials</h3>
            <p class="mb-4 text-xs text-gray-500 dark:text-gray-400">Set/check/delete credentials by auth reference (e.g. auth:openai:default).</p>
            <div class="grid gap-3 lg:grid-cols-[1fr_2fr_auto_auto_auto]">
              <input v-model="credentialName" type="text" class="field-input" placeholder="auth:openai:default" />
              <input v-model="credentialValue" type="password" class="field-input" placeholder="credential value" />
              <button type="button" class="rounded-lg bg-blue-600 px-3 py-2 text-xs font-semibold text-white hover:bg-blue-500" @click="setCredential">Set</button>
              <button type="button" class="rounded-lg border border-gray-200 px-3 py-2 text-xs text-gray-700 hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800" @click="checkCredential">Check</button>
              <button type="button" class="danger-btn" @click="deleteCredential">Delete</button>
            </div>
            <div class="mt-3 flex items-center gap-2">
              <button type="button" class="rounded-lg border border-gray-200 px-3 py-1.5 text-xs text-gray-700 hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800" @click="refreshCredentials">Refresh list</button>
              <span class="text-xs text-gray-500 dark:text-gray-400">{{ credentials.length }} stored</span>
            </div>
            <ul class="mt-3 max-h-40 space-y-1 overflow-auto rounded-lg border border-gray-200 p-2 text-xs dark:border-gray-700">
              <li v-for="name in credentials" :key="name" class="font-mono text-gray-700 dark:text-gray-300">{{ name }}</li>
              <li v-if="!credentials.length" class="text-gray-500 dark:text-gray-400">No stored credentials.</li>
            </ul>
          </div>

          <div class="rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
            <h3 class="mb-2 text-sm font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">OAuth</h3>
            <p class="mb-4 text-xs text-gray-500 dark:text-gray-400">OpenAI is one-click. Anthropic is two-step (start, then complete with code).</p>
            <div class="flex flex-wrap gap-2">
              <button type="button" class="rounded-lg bg-blue-600 px-3 py-2 text-xs font-semibold text-white hover:bg-blue-500 disabled:opacity-50" :disabled="oauthBusy" @click="loginOpenAI">Login OpenAI</button>
              <button type="button" class="rounded-lg bg-blue-600 px-3 py-2 text-xs font-semibold text-white hover:bg-blue-500 disabled:opacity-50" :disabled="oauthBusy" @click="startAnthropic">Start Anthropic Login</button>
            </div>
            <div v-if="anthropicUrl" class="mt-3 space-y-2 rounded-lg border border-gray-200 p-3 dark:border-gray-700">
              <a :href="anthropicUrl" target="_blank" rel="noreferrer" class="block truncate text-xs text-blue-600 hover:text-blue-500 dark:text-blue-400">{{ anthropicUrl }}</a>
              <div class="flex gap-2">
                <input v-model="anthropicCode" type="text" class="field-input" placeholder="Anthropic code" />
                <button type="button" class="rounded-lg bg-blue-600 px-3 py-2 text-xs font-semibold text-white hover:bg-blue-500 disabled:opacity-50" :disabled="oauthBusy || !anthropicCode.trim()" @click="completeAnthropic">Complete</button>
              </div>
            </div>
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
import { onMounted, ref, watch } from "vue";
import AppLayout from "../components/AppLayout.vue";
import { useMCP } from "../composables/useMCP";
import {
	type AgentEntry,
	type AgentTask,
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

const tabs: Tab[] = ["general", "agents", "sessions", "providers", "memory"];
const activeTab = ref<Tab>("general");

const store = useSettingsStore();
const { callTool } = useMCP();

const loading = ref(false);
const saving = ref(false);
const errorMessage = ref("");
const okMessage = ref("");

const draft = ref<AppConfig>(emptyConfig());
const providerRows = ref<{ name: string; auth: string }[]>([]);

const fallbacksCsv = ref("");
const concurrencyInput = ref("auto");

const sessionAgent = ref("");
const sessions = ref<SessionRow[]>([]);
const sessionLoading = ref(false);

const credentials = ref<string[]>([]);
const credentialName = ref("auth:openai:default");
const credentialValue = ref("");

const oauthBusy = ref(false);
const anthropicUrl = ref("");
const anthropicCode = ref("");

const allJobs = ref<JobEntry[]>([]);
const jobsLoading = ref(false);

const memoryAgent = ref("");
const notesContent = ref("");
const memoryLoading = ref(false);
const memoryClearing = ref(false);
const notesSaving = ref(false);
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
	await loadConfig();
	await refreshCredentials();
	void loadAllJobs();
});

function emptyConfig(): AppConfig {
	return {
		server: { port: 16677, tls: { cert: "", key: "" } },
		agents: [],
		models: { providers: {}, defaults: { model: "", fallbacks: [] } },
		browser: { binary: "", cdp_port: 9222 },
		scheduler: { concurrency: "auto" },
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
		fallbacksCsv.value = (cfg.models.defaults.fallbacks ?? []).join(", ");
		concurrencyInput.value = String(cfg.scheduler.concurrency ?? "auto");
		providerRows.value = Object.entries(cfg.models.providers ?? {}).map(
			([name, p]) => ({ name, auth: p.auth ?? "" }),
		);

		if (!draft.value.agents.length) {
			await importAgents();
		}

		if (!sessionAgent.value && draft.value.agents.length) {
			sessionAgent.value = draft.value.agents[0].name;
		}
		if (!memoryAgent.value && draft.value.agents.length) {
			memoryAgent.value = draft.value.agents[0].name;
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

function agentFallbacks(agent: AgentEntry): string {
	return (agent.fallbacks ?? []).join(", ");
}

function setAgentFallbacks(agent: AgentEntry, event: Event) {
	const value = (event.target as HTMLInputElement).value;
	agent.fallbacks = splitCsv(value);
}

function splitCsv(value: string): string[] {
	return value
		.split(",")
		.map((v) => v.trim())
		.filter(Boolean);
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
		normalized.models.defaults.fallbacks = splitCsv(fallbacksCsv.value);

		const conc = concurrencyInput.value.trim();
		if (conc.toLowerCase() === "auto") {
			normalized.scheduler.concurrency = "auto";
		} else {
			const n = Number.parseInt(conc, 10);
			normalized.scheduler.concurrency = Number.isNaN(n) || n < 1 ? "auto" : n;
		}

		normalized.models.providers = Object.fromEntries(
			providerRows.value
				.map((row) => ({ name: row.name.trim(), auth: row.auth.trim() }))
				.filter((row) => row.name !== "")
				.map((row) => [row.name, { auth: row.auth }]),
		);

		// Normalize agent/task values.
		normalized.agents = (normalized.agents ?? []).map((agent) => ({
			...agent,
			name: (agent.name ?? "").trim(),
			model: (agent.model ?? "").trim(),
			memory: (agent.memory ?? "").trim(),
			rules: (agent.rules ?? "").trim() || undefined,
			fallbacks: (agent.fallbacks ?? []).map((v) => v.trim()).filter(Boolean),
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

async function setCredential() {
	if (!credentialName.value.trim()) return;
	errorMessage.value = "";
	okMessage.value = "";
	try {
		await callTool("auth_set", {
			name: credentialName.value.trim(),
			value: credentialValue.value,
		});
		credentialValue.value = "";
		await refreshCredentials();
		okMessage.value = `Credential stored: ${credentialName.value.trim()}`;
	} catch (e) {
		errorMessage.value = e instanceof Error ? e.message : String(e);
	}
}

async function checkCredential() {
	if (!credentialName.value.trim()) return;
	errorMessage.value = "";
	okMessage.value = "";
	try {
		const raw = await callTool("auth_get", {
			name: credentialName.value.trim(),
		});
		const parsed = JSON.parse(raw) as { preview?: string };
		okMessage.value = `Credential is set: ${parsed.preview ?? "(masked)"}`;
	} catch (e) {
		errorMessage.value = e instanceof Error ? e.message : String(e);
	}
}

async function deleteCredential() {
	if (!credentialName.value.trim()) return;
	errorMessage.value = "";
	okMessage.value = "";
	try {
		await callTool("auth_delete", { name: credentialName.value.trim() });
		await refreshCredentials();
		okMessage.value = `Credential deleted: ${credentialName.value.trim()}`;
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
