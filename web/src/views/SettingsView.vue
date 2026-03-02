<template>
  <AppLayout>
    <div class="h-full overflow-y-auto">
      <div class="mx-auto max-w-6xl px-6 py-6">
        <div class="mb-6 flex items-center justify-between gap-4">
          <h2 class="text-xl font-bold text-gray-900 dark:text-white">Settings</h2>
          <button
            :disabled="store.saving || !form"
            class="rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white hover:bg-blue-500 disabled:opacity-50"
            @click="saveSettings"
          >{{ store.saving ? "Saving…" : "Save Changes" }}</button>
        </div>

        <div class="mb-6 flex flex-wrap gap-2">
          <button
            v-for="tab in tabs"
            :key="tab.key"
            type="button"
            :class="tabButtonClass(tab.key)"
            @click="selectTab(tab.key)"
          >{{ tab.label }}</button>
        </div>

        <div v-if="store.loading" class="text-sm text-gray-500 dark:text-gray-400">Loading…</div>
        <div v-else-if="store.error && !form" class="text-sm text-red-500 dark:text-red-400">Error: {{ store.error }}</div>

        <div v-else-if="form" class="space-y-10 pb-10">
          <section v-if="activeTab === 'general'" class="space-y-10">
            <section>
              <h3 class="mb-4 text-sm font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Server</h3>
              <div class="space-y-5 rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
                <FieldRow label="Port" :hint="`Port the server listens on (${portSchema.minimum}–${portSchema.maximum})`">
                  <input
                    v-model.number="form.server.port"
                    type="number"
                    :min="portSchema.minimum"
                    :max="portSchema.maximum"
                    :placeholder="String(portSchema.default)"
                    class="field-input w-32"
                  />
                </FieldRow>
                <FieldRow label="TLS Certificate" hint="Path to PEM certificate file (leave blank for self-signed)">
                  <input v-model="form.server.tls.cert" type="text" placeholder="/path/to/cert.pem" class="field-input" />
                </FieldRow>
                <FieldRow label="TLS Key" hint="Path to PEM private key file">
                  <input v-model="form.server.tls.key" type="text" placeholder="/path/to/key.pem" class="field-input" />
                </FieldRow>
              </div>
            </section>

            <section>
              <h3 class="mb-4 text-sm font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Models</h3>
              <div class="space-y-5 rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
                <FieldRow label="Default model" hint="Used when an agent does not specify a model">
                  <input v-model="form.models.defaults.model" type="text" class="field-input" />
                </FieldRow>
                <FieldRow label="Default fallbacks" hint="Models tried in order if the primary fails">
                  <StringList v-model="form.models.defaults.fallbacks" placeholder="openai/gpt-4o-mini" />
                </FieldRow>
              </div>
            </section>

            <section>
              <h3 class="mb-4 text-sm font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Browser</h3>
              <div class="space-y-5 rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
                <FieldRow label="Binary path" hint="Path to Chromium or Chrome executable">
                  <input v-model="form.browser.binary" type="text" placeholder="/usr/bin/chromium" class="field-input" />
                </FieldRow>
                <FieldRow label="CDP port" :hint="`Chrome DevTools Protocol port (${cdpSchema.minimum}–${cdpSchema.maximum})`">
                  <input
                    v-model.number="form.browser.cdp_port"
                    type="number"
                    :min="cdpSchema.minimum"
                    :max="cdpSchema.maximum"
                    :placeholder="String(cdpSchema.default)"
                    class="field-input w-32"
                  />
                </FieldRow>
              </div>
            </section>

            <section>
              <h3 class="mb-4 text-sm font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Scheduler</h3>
              <div class="space-y-5 rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
                <FieldRow label="Concurrency" hint='"auto" uses all available CPU cores; set a number for a fixed limit'>
                  <div class="flex flex-wrap items-center gap-4">
                    <label class="flex cursor-pointer items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
                      <input v-model="concurrencyMode" type="radio" value="auto" class="accent-blue-600" />
                      auto
                    </label>
                    <label class="flex cursor-pointer items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
                      <input v-model="concurrencyMode" type="radio" value="fixed" class="accent-blue-600" />
                      Fixed:
                      <input
                        v-model.number="concurrencyValue"
                        type="number"
                        min="1"
                        :disabled="concurrencyMode !== 'fixed'"
                        class="field-input w-20 disabled:opacity-40"
                      />
                    </label>
                  </div>
                </FieldRow>
              </div>
            </section>
          </section>

          <section v-else-if="activeTab === 'agents'" class="space-y-6">
            <div class="flex items-center justify-between">
              <h3 class="text-sm font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Agents & Tasks</h3>
              <button type="button" class="rounded-lg bg-blue-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-blue-500" @click="addAgent">+ Add Agent</button>
            </div>

            <div v-if="!form.agents.length" class="rounded-xl border border-gray-200 bg-white px-4 py-3 text-sm text-gray-500 dark:border-gray-800 dark:bg-gray-900 dark:text-gray-400">
              No agents configured.
            </div>

            <div v-for="(agent, agentIndex) in form.agents" :key="`agent-${agentIndex}`" class="space-y-4 rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
              <div class="flex items-start justify-between gap-4">
                <div class="grid flex-1 grid-cols-1 gap-4 lg:grid-cols-3">
                  <div>
                    <label class="field-label">Name</label>
                    <input v-model="agent.name" type="text" class="field-input" placeholder="assistant" />
                  </div>
                  <div>
                    <label class="field-label">Model</label>
                    <input v-model="agent.model" type="text" class="field-input" placeholder="anthropic/claude-sonnet-4-5" />
                  </div>
                  <div>
                    <label class="field-label">Memory</label>
                    <input v-model="agent.memory" type="text" class="field-input" placeholder="optional memory config" />
                  </div>
                </div>
                <button type="button" class="danger-btn" @click="removeAgent(agentIndex)">Remove Agent</button>
              </div>

              <div>
                <label class="field-label">Fallbacks</label>
                <StringList v-model="agent.fallbacks" placeholder="openai/gpt-4o-mini" />
              </div>

              <div class="space-y-3">
                <div class="flex items-center justify-between">
                  <h4 class="text-sm font-semibold text-gray-800 dark:text-gray-200">Tasks</h4>
                  <button type="button" class="rounded-lg border border-gray-200 px-3 py-1.5 text-xs text-gray-700 hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800" @click="addTask(agentIndex)">+ Add Task</button>
                </div>

                <div v-if="!(agent.tasks?.length)" class="rounded-lg border border-dashed border-gray-300 px-3 py-2 text-xs text-gray-500 dark:border-gray-700 dark:text-gray-400">
                  No tasks configured for this agent.
                </div>

                <div v-for="(task, taskIndex) in agent.tasks" :key="`task-${agentIndex}-${taskIndex}`" class="space-y-3 rounded-lg border border-gray-200 p-4 dark:border-gray-700">
                  <div class="flex items-start justify-between gap-4">
                    <div class="grid flex-1 grid-cols-1 gap-3 lg:grid-cols-2">
                      <div>
                        <label class="field-label">Task Name</label>
                        <input v-model="task.name" type="text" class="field-input" placeholder="daily-summary" />
                      </div>
                      <div>
                        <label class="field-label">Channel</label>
                        <input v-model="task.channel" type="text" class="field-input" placeholder="optional channel" />
                      </div>
                      <div>
                        <label class="field-label">Schedule (cron)</label>
                        <input v-model="task.schedule" type="text" class="field-input" placeholder="0 */30 * * * *" />
                      </div>
                      <div>
                        <label class="field-label">Watch (glob)</label>
                        <input v-model="task.watch" type="text" class="field-input" placeholder="./docs/**/*.md" />
                      </div>
                      <div>
                        <label class="field-label">Start At (RFC3339)</label>
                        <input v-model="task.start_at" type="text" class="field-input" placeholder="2026-03-01T10:00:00Z" />
                      </div>
                      <label class="mt-6 flex items-center gap-2 text-xs text-gray-600 dark:text-gray-400">
                        <input v-model="task.run_once" type="checkbox" class="accent-blue-600" />
                        Run once
                      </label>
                    </div>
                    <button type="button" class="danger-btn" @click="removeTask(agentIndex, taskIndex)">Remove Task</button>
                  </div>

                  <div>
                    <label class="field-label">Prompt</label>
                    <textarea v-model="task.prompt" rows="3" class="field-input" placeholder="Task prompt..."></textarea>
                  </div>
                </div>
              </div>
            </div>
          </section>

          <section v-else-if="activeTab === 'sessions'" class="space-y-6">
            <div class="rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
              <div class="flex flex-wrap items-end gap-3">
                <div>
                  <label class="field-label">Agent</label>
                  <select v-model="sessionsAgent" class="field-input min-w-56">
                    <option value="" disabled>Select an agent</option>
                    <option v-for="agent in form.agents" :key="`sess-agent-${agent.name}`" :value="agent.name">{{ agent.name }}</option>
                  </select>
                </div>
                <button type="button" class="rounded-lg border border-gray-200 px-3 py-2 text-xs text-gray-700 hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800" :disabled="!sessionsAgent || sessionsLoading" @click="loadSessions">{{ sessionsLoading ? "Loading…" : "Refresh Sessions" }}</button>
                <button type="button" class="rounded-lg bg-blue-600 px-3 py-2 text-xs font-semibold text-white hover:bg-blue-500 disabled:opacity-50" :disabled="!sessionsAgent || sessionsLoading" @click="createSession">+ Create Session</button>
              </div>

              <p v-if="sessionsError" class="mt-3 text-xs text-red-500 dark:text-red-400">{{ sessionsError }}</p>

              <div class="mt-4 overflow-hidden rounded-lg border border-gray-200 dark:border-gray-700">
                <table v-if="sessions.length" class="w-full text-sm">
                  <thead>
                    <tr class="border-b border-gray-200 text-left text-xs font-medium text-gray-500 dark:border-gray-700 dark:text-gray-400">
                      <th class="px-3 py-2">Name</th>
                      <th class="px-3 py-2">ID</th>
                      <th class="px-3 py-2">Updated</th>
                      <th class="px-3 py-2">Processing</th>
                      <th class="px-3 py-2">Action</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="session in sessions" :key="session.id" class="border-b border-gray-100 text-gray-700 dark:border-gray-800 dark:text-gray-300">
                      <td class="px-3 py-2">{{ session.name || "—" }}</td>
                      <td class="px-3 py-2 font-mono text-xs text-gray-500 dark:text-gray-400">{{ session.id.slice(-10) }}</td>
                      <td class="px-3 py-2 text-xs">{{ formatDate(session.updated_at) }}</td>
                      <td class="px-3 py-2">
                        <span :class="session.is_processing ? 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900 dark:text-yellow-300' : 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-300'" class="rounded-full px-2 py-0.5 text-xs">
                          {{ session.is_processing ? "yes" : "no" }}
                        </span>
                      </td>
                      <td class="px-3 py-2">
                        <button type="button" class="rounded border border-red-200 px-2 py-1 text-xs text-red-600 hover:bg-red-50 dark:border-red-900 dark:text-red-400 dark:hover:bg-red-950" @click="stopSession(session.id)">Stop</button>
                      </td>
                    </tr>
                  </tbody>
                </table>
                <div v-else class="px-3 py-3 text-xs text-gray-500 dark:text-gray-400">No sessions found for this agent.</div>
              </div>
            </div>
          </section>

          <section v-else-if="activeTab === 'providers'" class="space-y-6">
            <div class="space-y-4 rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
              <h3 class="text-sm font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Provider Mapping</h3>
              <ProviderList v-model="providerEntries" />
            </div>

            <div class="space-y-4 rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
              <h3 class="text-sm font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Credentials</h3>
              <div class="grid grid-cols-1 gap-3 lg:grid-cols-[1fr_2fr_auto_auto_auto]">
                <input v-model="credentialName" type="text" class="field-input" placeholder="auth:openai:default" />
                <input v-model="credentialValue" type="password" class="field-input" placeholder="credential value" />
                <button type="button" class="rounded-lg bg-blue-600 px-3 py-2 text-xs font-semibold text-white hover:bg-blue-500" @click="setCredential">Set</button>
                <button type="button" class="rounded-lg border border-gray-200 px-3 py-2 text-xs text-gray-700 hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800" @click="checkCredential">Check</button>
                <button type="button" class="rounded-lg border border-red-200 px-3 py-2 text-xs text-red-600 hover:bg-red-50 dark:border-red-900 dark:text-red-400 dark:hover:bg-red-950" @click="deleteCredential">Delete</button>
              </div>
              <div class="flex items-center gap-2">
                <button type="button" class="rounded-lg border border-gray-200 px-3 py-1.5 text-xs text-gray-700 hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800" @click="refreshCredentials">Refresh Stored Keys</button>
                <span class="text-xs text-gray-500 dark:text-gray-400">{{ credentials.length }} stored</span>
              </div>
              <ul class="max-h-40 space-y-1 overflow-auto rounded-lg border border-gray-200 p-2 text-xs dark:border-gray-700">
                <li v-for="name in credentials" :key="name" class="font-mono text-gray-700 dark:text-gray-300">{{ name }}</li>
                <li v-if="!credentials.length" class="text-gray-500 dark:text-gray-400">No stored credentials.</li>
              </ul>
              <p v-if="credentialStatus" class="text-xs text-gray-600 dark:text-gray-300">{{ credentialStatus }}</p>
              <p v-if="credentialError" class="text-xs text-red-500 dark:text-red-400">{{ credentialError }}</p>
            </div>

            <div class="space-y-4 rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
              <h3 class="text-sm font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">OAuth</h3>
              <div class="flex flex-wrap gap-2">
                <button type="button" class="rounded-lg bg-blue-600 px-3 py-2 text-xs font-semibold text-white hover:bg-blue-500" :disabled="oauthBusy" @click="loginOpenAI">Login OpenAI</button>
                <button type="button" class="rounded-lg bg-blue-600 px-3 py-2 text-xs font-semibold text-white hover:bg-blue-500" :disabled="oauthBusy" @click="startAnthropicLogin">Start Anthropic Login</button>
              </div>

              <div v-if="anthropicLoginUrl" class="space-y-2 rounded-lg border border-gray-200 p-3 dark:border-gray-700">
                <p class="text-xs text-gray-600 dark:text-gray-300">Open this URL, complete login, and paste the displayed code:</p>
                <a :href="anthropicLoginUrl" target="_blank" rel="noreferrer" class="block truncate text-xs text-blue-600 hover:text-blue-500 dark:text-blue-400">{{ anthropicLoginUrl }}</a>
                <div class="flex gap-2">
                  <input v-model="anthropicCode" type="text" class="field-input" placeholder="Anthropic code" />
                  <button type="button" class="rounded-lg bg-blue-600 px-3 py-2 text-xs font-semibold text-white hover:bg-blue-500" :disabled="oauthBusy || !anthropicCode.trim()" @click="completeAnthropicLogin">Complete</button>
                </div>
              </div>

              <p v-if="oauthStatus" class="text-xs text-gray-600 dark:text-gray-300">{{ oauthStatus }}</p>
              <p v-if="oauthError" class="text-xs text-red-500 dark:text-red-400">{{ oauthError }}</p>
            </div>
          </section>

          <div v-if="saveSuccess" class="rounded-lg bg-green-50 px-4 py-3 text-sm text-green-700 dark:bg-green-950 dark:text-green-300">Settings saved successfully.</div>
          <div v-if="saveError" class="rounded-lg bg-red-50 px-4 py-3 text-sm text-red-700 dark:bg-red-950 dark:text-red-300">{{ saveError }}</div>
        </div>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import AppLayout from "../components/AppLayout.vue";
import { useMCP } from "../composables/useMCP";
import schema from "../config-schema.json";
import { type AgentTask, type AppConfig, useSettingsStore } from "../stores/settings";

type SettingsTab = "general" | "agents" | "sessions" | "providers";

interface Session {
  id: string;
  agent_id: string;
  name: string;
  created_at: string;
  updated_at: string;
  is_processing?: boolean;
}

const tabs: { key: SettingsTab; label: string }[] = [
  { key: "general", label: "General" },
  { key: "agents", label: "Agents & Tasks" },
  { key: "sessions", label: "Sessions" },
  { key: "providers", label: "Providers & Auth" },
];

const store = useSettingsStore();
const { callTool } = useMCP();
const route = useRoute();
const router = useRouter();

const sch = schema as {
  properties: {
    server: { properties: { port: { minimum: number; maximum: number; default: number } } };
    browser: { properties: { cdp_port: { minimum: number; maximum: number; default: number } } };
  };
};
const portSchema = sch.properties.server.properties.port;
const cdpSchema = sch.properties.browser.properties.cdp_port;

const form = ref<AppConfig | null>(null);
const saveSuccess = ref(false);
const saveError = ref("");

const activeTab = ref<SettingsTab>("general");

const concurrencyMode = ref<"auto" | "fixed">("auto");
const concurrencyValue = ref<number>(1);
const providerEntries = ref<{ name: string; auth: string }[]>([]);

const sessionsAgent = ref("");
const sessions = ref<Session[]>([]);
const sessionsLoading = ref(false);
const sessionsError = ref("");

const credentials = ref<string[]>([]);
const credentialName = ref("auth:openai:default");
const credentialValue = ref("");
const credentialStatus = ref("");
const credentialError = ref("");

const oauthBusy = ref(false);
const oauthStatus = ref("");
const oauthError = ref("");
const anthropicLoginUrl = ref("");
const anthropicCode = ref("");

const selectedTabFromRoute = computed<SettingsTab>(() => {
  const candidate = route.query.tab;
  if (candidate === "agents" || candidate === "sessions" || candidate === "providers" || candidate === "general") {
    return candidate;
  }
  return "general";
});

watch(
  selectedTabFromRoute,
  (tab) => {
    activeTab.value = tab;
  },
  { immediate: true },
);

watch(
  () => store.config,
  (cfg) => {
    if (!cfg) return;
    form.value = JSON.parse(JSON.stringify(cfg)) as AppConfig;

    const c = cfg.scheduler.concurrency;
    if (c === "auto" || c === "" || c == null) {
      concurrencyMode.value = "auto";
      concurrencyValue.value = 1;
    } else {
      concurrencyMode.value = "fixed";
      concurrencyValue.value = Number(c);
    }

    providerEntries.value = Object.entries(cfg.models.providers ?? {}).map(([name, p]) => ({
      name,
      auth: p.auth,
    }));

    if (!sessionsAgent.value && cfg.agents.length > 0) {
      sessionsAgent.value = cfg.agents[0].name;
    }
  },
  { immediate: true },
);

onMounted(async () => {
  if (!store.config) {
    await store.fetchConfig();
  }
  await refreshCredentials();
});

function tabButtonClass(key: SettingsTab): string {
  return activeTab.value === key
    ? "rounded-lg bg-gray-900 px-3 py-1.5 text-xs font-semibold text-white dark:bg-white dark:text-gray-900"
    : "rounded-lg border border-gray-200 px-3 py-1.5 text-xs font-medium text-gray-600 hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800";
}

function selectTab(tab: SettingsTab) {
  router.replace({ query: { ...route.query, tab } });
}

function addAgent() {
  if (!form.value) return;
  form.value.agents.push({
    name: "",
    model: "",
    memory: "",
    fallbacks: [],
    channels: [],
    tasks: [],
  });
}

function removeAgent(index: number) {
  if (!form.value) return;
  form.value.agents.splice(index, 1);
}

function addTask(agentIndex: number) {
  if (!form.value) return;
  const task: AgentTask = {
    name: "",
    prompt: "",
    schedule: "",
    watch: "",
    start_at: "",
    run_once: false,
    channel: "",
  };
  if (!Array.isArray(form.value.agents[agentIndex].tasks)) {
    form.value.agents[agentIndex].tasks = [];
  }
  form.value.agents[agentIndex].tasks.push(task);
}

function removeTask(agentIndex: number, taskIndex: number) {
  if (!form.value) return;
  form.value.agents[agentIndex].tasks.splice(taskIndex, 1);
}

async function saveSettings() {
  if (!form.value) return;
  saveSuccess.value = false;
  saveError.value = "";

  form.value.scheduler.concurrency = concurrencyMode.value === "auto" ? "auto" : concurrencyValue.value;
  form.value.models.providers = Object.fromEntries(
    providerEntries.value
      .filter((entry) => entry.name.trim() !== "")
      .map((entry) => [entry.name.trim(), { auth: entry.auth }]),
  );

  try {
    await store.saveConfig(form.value);
    saveSuccess.value = true;
    setTimeout(() => (saveSuccess.value = false), 3000);
  } catch (e) {
    saveError.value = e instanceof Error ? e.message : String(e);
  }
}

async function loadSessions() {
  if (!sessionsAgent.value) return;
  sessionsLoading.value = true;
  sessionsError.value = "";
  try {
    const raw = await callTool("session_list", { agent: sessionsAgent.value });
    sessions.value = (JSON.parse(raw) as Session[]) ?? [];
  } catch (e) {
    sessions.value = [];
    sessionsError.value = e instanceof Error ? e.message : String(e);
  } finally {
    sessionsLoading.value = false;
  }
}

async function createSession() {
  if (!sessionsAgent.value) return;
  sessionsLoading.value = true;
  sessionsError.value = "";
  try {
    await callTool("session_create", { agent: sessionsAgent.value });
    await loadSessions();
  } catch (e) {
    sessionsError.value = e instanceof Error ? e.message : String(e);
  } finally {
    sessionsLoading.value = false;
  }
}

async function stopSession(sessionID: string) {
  sessionsError.value = "";
  try {
    await callTool("session_stop", { session_id: sessionID });
    await loadSessions();
  } catch (e) {
    sessionsError.value = e instanceof Error ? e.message : String(e);
  }
}

function formatDate(value: string): string {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

async function refreshCredentials() {
  credentialError.value = "";
  try {
    const raw = await callTool("auth_list");
    credentials.value = (JSON.parse(raw) as string[]) ?? [];
  } catch (e) {
    credentials.value = [];
    credentialError.value = e instanceof Error ? e.message : String(e);
  }
}

async function setCredential() {
  credentialError.value = "";
  credentialStatus.value = "";
  if (!credentialName.value.trim()) {
    credentialError.value = "Credential name is required.";
    return;
  }
  try {
    await callTool("auth_set", {
      name: credentialName.value.trim(),
      value: credentialValue.value,
    });
    credentialStatus.value = `Stored ${credentialName.value.trim()}.`;
    credentialValue.value = "";
    await refreshCredentials();
  } catch (e) {
    credentialError.value = e instanceof Error ? e.message : String(e);
  }
}

async function checkCredential() {
  credentialError.value = "";
  credentialStatus.value = "";
  if (!credentialName.value.trim()) {
    credentialError.value = "Credential name is required.";
    return;
  }
  try {
    const raw = await callTool("auth_get", { name: credentialName.value.trim() });
    const parsed = JSON.parse(raw) as { set?: boolean; preview?: string };
    credentialStatus.value = parsed.set
      ? `Credential found: ${parsed.preview ?? "(masked)"}`
      : "Credential not set.";
  } catch (e) {
    credentialError.value = e instanceof Error ? e.message : String(e);
  }
}

async function deleteCredential() {
  credentialError.value = "";
  credentialStatus.value = "";
  if (!credentialName.value.trim()) {
    credentialError.value = "Credential name is required.";
    return;
  }
  try {
    await callTool("auth_delete", { name: credentialName.value.trim() });
    credentialStatus.value = `Deleted ${credentialName.value.trim()}.`;
    await refreshCredentials();
  } catch (e) {
    credentialError.value = e instanceof Error ? e.message : String(e);
  }
}

async function loginOpenAI() {
  oauthBusy.value = true;
  oauthStatus.value = "";
  oauthError.value = "";
  try {
    const result = await callTool("auth_login_openai");
    oauthStatus.value = result || "OpenAI OAuth login complete.";
    await refreshCredentials();
  } catch (e) {
    oauthError.value = e instanceof Error ? e.message : String(e);
  } finally {
    oauthBusy.value = false;
  }
}

async function startAnthropicLogin() {
  oauthBusy.value = true;
  oauthStatus.value = "";
  oauthError.value = "";
  anthropicLoginUrl.value = "";
  try {
    const raw = await callTool("auth_login_anthropic");
    const parsed = JSON.parse(raw) as { url?: string; instructions?: string };
    anthropicLoginUrl.value = parsed.url ?? "";
    oauthStatus.value = parsed.instructions ?? "Anthropic login started.";
  } catch (e) {
    oauthError.value = e instanceof Error ? e.message : String(e);
  } finally {
    oauthBusy.value = false;
  }
}

async function completeAnthropicLogin() {
  oauthBusy.value = true;
  oauthStatus.value = "";
  oauthError.value = "";
  try {
    const result = await callTool("auth_login_anthropic_complete", {
      code: anthropicCode.value.trim(),
    });
    oauthStatus.value = result || "Anthropic OAuth login complete.";
    anthropicCode.value = "";
    await refreshCredentials();
  } catch (e) {
    oauthError.value = e instanceof Error ? e.message : String(e);
  } finally {
    oauthBusy.value = false;
  }
}

const FieldRow = defineComponent({
  props: { label: String, hint: String },
  setup(props, { slots }) {
    return () =>
      h("div", { class: "grid grid-cols-[180px_1fr] items-start gap-4" }, [
        h("div", { class: "pt-2" }, [
          h("label", { class: "block text-sm font-medium text-gray-700 dark:text-gray-300" }, props.label),
          props.hint
            ? h("p", { class: "mt-0.5 text-xs text-gray-400 dark:text-gray-500" }, props.hint)
            : null,
        ]),
        h("div", { class: "min-w-0" }, slots.default?.()),
      ]);
  },
});

const StringList = defineComponent({
  props: {
    modelValue: { type: Array as () => string[], required: true },
    placeholder: String,
  },
  emits: ["update:modelValue"],
  setup(props, { emit }) {
    function update(fn: (arr: string[]) => string[]) {
      emit("update:modelValue", fn([...props.modelValue]));
    }
    function remove(i: number) {
      update((arr) => {
        arr.splice(i, 1);
        return arr;
      });
    }
    function add() {
      update((arr) => {
        arr.push("");
        return arr;
      });
    }
    function setItem(i: number, value: string) {
      update((arr) => {
        arr[i] = value;
        return arr;
      });
    }

    return () =>
      h("div", { class: "space-y-2" }, [
        ...props.modelValue.map((item, i) =>
          h("div", { key: i, class: "flex items-center gap-1.5" }, [
            h("input", {
              value: item,
              type: "text",
              placeholder: props.placeholder ?? "",
              class: "field-input flex-1",
              onInput: (e: Event) => setItem(i, (e.target as HTMLInputElement).value),
            }),
            h(
              "button",
              {
                type: "button",
                class: "list-btn text-red-500 hover:text-red-600",
                title: "Remove",
                onClick: () => remove(i),
              },
              "×",
            ),
          ]),
        ),
        h(
          "button",
          {
            type: "button",
            class: "mt-1 text-xs font-medium text-blue-600 hover:text-blue-500 dark:text-blue-400",
            onClick: add,
          },
          "+ Add entry",
        ),
      ]);
  },
});

const ProviderList = defineComponent({
  props: {
    modelValue: {
      type: Array as () => { name: string; auth: string }[],
      required: true,
    },
  },
  emits: ["update:modelValue"],
  setup(props, { emit }) {
    function update(
      fn: (arr: { name: string; auth: string }[]) => { name: string; auth: string }[],
    ) {
      emit("update:modelValue", fn([...props.modelValue]));
    }
    function remove(i: number) {
      update((arr) => {
        arr.splice(i, 1);
        return arr;
      });
    }
    function add() {
      update((arr) => {
        arr.push({ name: "", auth: "" });
        return arr;
      });
    }
    function setField(i: number, field: "name" | "auth", value: string) {
      update((arr) => {
        arr[i] = { ...arr[i], [field]: value };
        return arr;
      });
    }

    return () =>
      h("div", { class: "space-y-2" }, [
        ...props.modelValue.map((entry, i) =>
          h("div", { key: i, class: "flex items-center gap-1.5" }, [
            h("input", {
              value: entry.name,
              type: "text",
              placeholder: "provider",
              class: "field-input w-36 font-mono text-xs",
              onInput: (e: Event) => setField(i, "name", (e.target as HTMLInputElement).value),
            }),
            h("span", { class: "text-gray-400 dark:text-gray-600" }, "→"),
            h("input", {
              value: entry.auth,
              type: "text",
              placeholder: "auth:provider:name",
              class: "field-input flex-1 text-xs",
              onInput: (e: Event) => setField(i, "auth", (e.target as HTMLInputElement).value),
            }),
            h(
              "button",
              {
                type: "button",
                class: "list-btn text-red-500 hover:text-red-600",
                title: "Remove",
                onClick: () => remove(i),
              },
              "×",
            ),
          ]),
        ),
        h(
          "button",
          {
            type: "button",
            class: "mt-1 text-xs font-medium text-blue-600 hover:text-blue-500 dark:text-blue-400",
            onClick: add,
          },
          "+ Add provider",
        ),
      ]);
  },
});

void [FieldRow, StringList, ProviderList, computed];
</script>

<style scoped>
@reference "../style.css";

.field-input {
  @apply w-full rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 placeholder-gray-400 focus:border-blue-500 focus:outline-none dark:border-gray-700 dark:bg-gray-800 dark:text-white dark:placeholder-gray-500;
}

.field-label {
  @apply mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400;
}

.list-btn {
  @apply flex h-7 w-7 items-center justify-center rounded border border-gray-200 text-xs text-gray-500 hover:bg-gray-100 disabled:cursor-not-allowed disabled:opacity-30 dark:border-gray-700 dark:text-gray-400 dark:hover:bg-gray-800;
}

.danger-btn {
  @apply rounded-lg border border-red-200 px-2.5 py-1.5 text-xs font-medium text-red-600 hover:bg-red-50 dark:border-red-900 dark:text-red-400 dark:hover:bg-red-950;
}
</style>
