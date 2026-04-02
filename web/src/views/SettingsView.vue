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


				<SettingsGeneralTab />
				<SettingsAgentsTab />
				<SettingsSkillsTab />
				<SettingsSessionsTab />
				<SettingsProvidersTab />


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
				class="fixed left-1/2 top-1/2 z-50 w-full max-w-lg -translate-x-1/2 -translate-y-1/2 rounded-xl bg-white p-6 shadow-2xl dark:bg-gray-900">
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
				class="fixed left-1/2 top-1/2 z-50 w-full max-w-lg -translate-x-1/2 -translate-y-1/2 rounded-xl bg-white p-6 shadow-2xl dark:bg-gray-900">
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

	<Teleport to="body">
		<div
			v-if="webSearchSecretModalOpen"
			class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 px-4 py-6"
			@click.self="closeWebSearchSecretModal"
		>
			<form
				class="w-full max-w-md rounded-xl border border-gray-200 bg-white p-6 shadow-2xl dark:border-gray-800 dark:bg-gray-900"
				@submit.prevent="saveWebSearchSecret"
			>
				<div class="flex items-start justify-between gap-4">
					<div>
						<h3 class="text-base font-bold text-gray-900 dark:text-white">{{ secretModalTitle }}</h3>
						<p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
							{{ secretModalDescription }}
						</p>
					</div>
					<button
						type="button"
						class="rounded-lg border border-gray-200 px-3 py-1.5 text-xs text-gray-600 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-400 dark:hover:bg-gray-800"
						@click="closeWebSearchSecretModal"
					>
						Cancel
					</button>
				</div>
				<div class="mt-5 space-y-4">
					<div>
						<label class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400">Secret name</label>
						<input
							v-model="webSearchSecretModalName"
							type="text"
							class="field-input font-mono text-sm"
							:placeholder="secretModalNamePlaceholder"
						/>
					</div>
					<div>
						<label class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400">{{ secretModalValueLabel }}</label>
						<input
							v-model="webSearchSecretModalValue"
							type="password"
							class="field-input text-sm"
							:placeholder="secretModalValuePlaceholder"
						/>
					</div>
				</div>
				<p v-if="webSearchSecretModalError" class="mt-3 text-xs text-red-500 dark:text-red-400">
					{{ webSearchSecretModalError }}
				</p>
				<div class="mt-6 flex justify-end gap-3">
					<button
						type="button"
						class="rounded-lg border border-gray-200 px-4 py-2 text-sm text-gray-600 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-400 dark:hover:bg-gray-800"
						@click="closeWebSearchSecretModal"
					>
						Cancel
					</button>
					<button
						type="submit"
						class="rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white hover:bg-blue-500 disabled:opacity-50"
						:disabled="webSearchSecretModalSaving || !webSearchSecretModalName.trim() || !webSearchSecretModalValue.trim()"
					>
						{{ webSearchSecretModalSaving ? "Saving..." : "Save Secret" }}
					</button>
				</div>
			</form>
		</div>
	</Teleport>
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
} from "radix-vue";
import {
    computed,
    nextTick,
    onMounted,
    onUnmounted,
    provide,
    proxyRefs,
    ref,
    watch,
} from "vue";
import { useRoute, useRouter } from "vue-router";
import AppLayout from "../components/AppLayout.vue";
import { settingsViewContextKey } from "../components/settings/context";
import SettingsAgentsTab from "../components/settings/SettingsAgentsTab.vue";
import SettingsGeneralTab from "../components/settings/SettingsGeneralTab.vue";
import SettingsProvidersTab from "../components/settings/SettingsProvidersTab.vue";
import SettingsSessionsTab from "../components/settings/SettingsSessionsTab.vue";
import SettingsSkillsTab from "../components/settings/SettingsSkillsTab.vue";
import { useAvailableModels } from "../composables/useAvailableModels";
import { type MCPToolInfo, useMCP } from "../composables/useMCP";
import {
    KNOWN_PROVIDERS,
    useProviderAuth,
} from "../composables/useProviderAuth";
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

const selectedChannelIdx = ref<number | null>(
	draft.value.agents[selectedAgentIdx.value]?.channels?.length ? 0 : null,
);

const selectedChannel = computed((): AgentChannel | null => {
	const agent = draft.value.agents[selectedAgentIdx.value];
	if (!agent?.channels?.length) return null;
	const idx = selectedChannelIdx.value ?? 0;
	if (idx < 0 || idx >= agent.channels.length) return agent.channels[0] ?? null;
	return agent.channels[idx] ?? null;
});

// Agent tab click → push new route (also resets subtab) and reset selectedTaskIdx for the new agent.
watch(selectedAgentIdx, (idx) => {
	const target = agentRoutePath(idx, selectedAgentSubtab.value);
	if (route.path !== target) void router.push(target);
	// Reset selectedTaskIdx for the newly-selected agent so a task is shown if available.
	const tasks = draft.value.agents[idx]?.tasks ?? [];
	selectedTaskIdx.value = tasks.length ? 0 : null;
	const channels = draft.value.agents[idx]?.channels ?? [];
	selectedChannelIdx.value = channels.length ? 0 : null;
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

watch(
	() => draft.value.agents[selectedAgentIdx.value]?.channels?.length,
	(len) => {
		if (
			len &&
			(selectedChannelIdx.value === null ||
				selectedChannelIdx.value === undefined)
		) {
			selectedChannelIdx.value = 0;
		} else if (!len) {
			selectedChannelIdx.value = null;
		} else if ((selectedChannelIdx.value ?? 0) >= len) {
			selectedChannelIdx.value = len - 1;
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

const {
	oauthBusy,
	anthropicUrl,
	anthropicCode,
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
	startAnthropic: authStartAnthropic,
	completeAnthropic: authCompleteAnthropic,
	startOpenAI: authStartOpenAI,
	completeOpenAI: authCompleteOpenAI,
	startGemini: authStartGemini,
	completeGemini: authCompleteGemini,
	startCopilot: authStartCopilot,
	completeCopilot: authCompleteCopilot,
} = useProviderAuth(callTool);
const providerAddSelection = ref("");
const providerApiKeyValue = ref("");
const secretName = ref("");
const secretValue = ref("");

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

const webSearchSecretModalOpen = ref(false);
const webSearchSecretModalName = ref("");
const webSearchSecretModalValue = ref("");
const webSearchSecretModalError = ref("");
const webSearchSecretModalSaving = ref(false);
const secretModalTitle = ref("Add New Secret");
const secretModalDescription = ref(
	"Store a Brave Search API key and select it for web search.",
);
const secretModalNamePlaceholder = ref("brave_search_api_key");
const secretModalValueLabel = ref("API key");
const secretModalValuePlaceholder = ref("BSA...");
let secretModalOnSave: ((name: string) => void) | null = null;

function generatedWebSearchSecretName(): string {
	const base = "brave_search_api_key";
	const existing = new Set(webSearchSecretOptions.value);
	if (!existing.has(base)) {
		return base;
	}
	let suffix = 2;
	while (existing.has(`${base}_${suffix}`)) {
		suffix += 1;
	}
	return `${base}_${suffix}`;
}

function generatedChannelSecretName(
	channelType: string,
	field: "token" | "url",
): string {
	const base =
		channelType === "slack"
			? field === "url"
				? "slack_app_token"
				: "slack_bot_token"
			: "discord_bot_token";
	const existing = new Set(webSearchSecretOptions.value);
	if (!existing.has(base)) {
		return base;
	}
	let suffix = 2;
	while (existing.has(`${base}_${suffix}`)) {
		suffix += 1;
	}
	return `${base}_${suffix}`;
}

function openSecretModal(options: {
	name: string;
	title: string;
	description: string;
	valueLabel: string;
	valuePlaceholder: string;
	onSave: (name: string) => void;
}) {
	webSearchSecretModalName.value = options.name;
	webSearchSecretModalValue.value = "";
	webSearchSecretModalError.value = "";
	secretModalTitle.value = options.title;
	secretModalDescription.value = options.description;
	secretModalNamePlaceholder.value = options.name;
	secretModalValueLabel.value = options.valueLabel;
	secretModalValuePlaceholder.value = options.valuePlaceholder;
	secretModalOnSave = options.onSave;
	webSearchSecretModalOpen.value = true;
}

function openWebSearchSecretModal() {
	openSecretModal({
		name: generatedWebSearchSecretName(),
		title: "Add New Secret",
		description: "Store a Brave Search API key and select it for web search.",
		valueLabel: "API key",
		valuePlaceholder: "BSA...",
		onSave: (name: string) => {
			webSearchSecretRef.value = `auth:${name}`;
		},
	});
}

function closeWebSearchSecretModal() {
	webSearchSecretModalOpen.value = false;
	webSearchSecretModalValue.value = "";
	webSearchSecretModalError.value = "";
	secretModalOnSave = null;
}

const webSearchSecretRef = computed({
	get(): string {
		return draft.value.search.web.brave_api_key?.trim() ?? "";
	},
	set(value: string) {
		draft.value.search.web.brave_api_key = value;
	},
});

function openChannelTokenSecretModal(
	channel: AgentChannel,
	field: "token" | "url",
) {
	const isSlackAppToken = channel.type === "slack" && field === "url";
	const isSlackBotToken = channel.type === "slack" && field === "token";
	openSecretModal({
		name: generatedChannelSecretName(channel.type, field),
		title: "Add New Secret",
		description: isSlackAppToken
			? "Store a Slack app-level Socket Mode token and select it for this channel."
			: isSlackBotToken
				? "Store a Slack bot token and select it for this channel."
				: "Store a Discord bot token and select it for this channel.",
		valueLabel: isSlackAppToken ? "App-level token" : "Bot token",
		valuePlaceholder: isSlackAppToken
			? "xapp-..."
			: channel.type === "slack"
				? "xoxb-..."
				: "Discord bot token",
		onSave: (name: string) => {
			if (field === "url") {
				channel.url = `auth:${name}`;
				return;
			}
			channel.token = `auth:${name}`;
		},
	});
}

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

function channelTypeLabel(channel: AgentChannel): string {
	switch ((channel.type || "").toLowerCase()) {
		case "slack":
			return "Slack";
		case "discord":
			return "Discord";
		case "signal":
			return "Signal";
		default:
			return channel.type || "Channel";
	}
}

function channelTypeIconClass(channel: AgentChannel, selected = false): string {
	const enabled = isChannelEnabled(channel);
	if (selected) {
		return enabled
			? "inline-flex items-center justify-center text-white dark:text-white"
			: "inline-flex items-center justify-center text-gray-500 dark:text-gray-400";
	}
	return enabled
		? "inline-flex items-center justify-center text-blue-700 dark:text-blue-300"
		: "inline-flex items-center justify-center text-gray-500 dark:text-gray-400";
}

function channelListButtonClass(
	channel: AgentChannel,
	selected: boolean,
): string {
	if (selected) {
		return isChannelEnabled(channel)
			? "bg-gray-900 text-white dark:bg-gray-700 dark:text-white"
			: "bg-gray-100 text-gray-500 ring-1 ring-inset ring-gray-300 dark:bg-gray-900 dark:text-gray-400 dark:ring-gray-700";
	}
	return isChannelEnabled(channel)
		? "text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-800"
		: "text-gray-400 hover:bg-gray-50 dark:text-gray-500 dark:hover:bg-gray-900";
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
	if (agentIndex === selectedAgentIdx.value) {
		selectedChannelIdx.value =
			draft.value.agents[agentIndex].channels.length - 1;
	}
}

function removeChannel(agentIndex: number, chIndex: number) {
	draft.value.agents[agentIndex].channels.splice(chIndex, 1);
	if (agentIndex !== selectedAgentIdx.value) return;
	const channels = draft.value.agents[agentIndex].channels ?? [];
	if (!channels.length) {
		selectedChannelIdx.value = null;
		return;
	}
	if ((selectedChannelIdx.value ?? 0) > chIndex) {
		selectedChannelIdx.value = (selectedChannelIdx.value ?? 0) - 1;
		return;
	}
	if ((selectedChannelIdx.value ?? 0) >= channels.length) {
		selectedChannelIdx.value = channels.length - 1;
	}
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

function formatCountdown(seconds: number | null): string {
	if (seconds == null) return "";
	const mins = Math.floor(seconds / 60);
	const secs = seconds % 60;
	return `${mins}:${String(secs).padStart(2, "0")}`;
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
	oauthBusy.value = true;
	errorMessage.value = "";
	okMessage.value = "";
	try {
		if (p.id === "anthropic") {
			await startAnthropic();
			okMessage.value =
				"Anthropic authorization started. Open the link below, then paste the code to complete it.";
		} else if (p.id === "openai-codex") {
			await loginOpenAI();
			okMessage.value =
				"OpenAI Codex authorization started. Complete it using the URL shown below, then click Complete.";
		} else if (p.id === "google") {
			await loginGemini();
			okMessage.value =
				"Gemini authorization started. Complete it using the URL shown below, then click Complete.";
		} else if (p.id === "github-copilot") {
			await startCopilot();
			return;
		}
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
			okMessage.value =
				"Anthropic re-authorization started. Open the link below, then paste the code to complete it.";
		} else if (provider === "openai" || provider === "openai-codex") {
			await loginOpenAI();
			okMessage.value =
				"OpenAI Codex re-authorization started. Complete it using the URL shown below, then click Complete.";
		} else if (provider === "google") {
			await loginGemini();
			okMessage.value =
				"Gemini re-authorization started. Complete it using the URL shown below, then click Complete.";
		} else if (provider === "github-copilot") {
			await startCopilot();
			return;
		}
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

async function saveWebSearchSecret() {
	const name = webSearchSecretModalName.value.trim().replace(/^auth:/, "");
	const value = webSearchSecretModalValue.value.trim();
	if (!name || !value) return;
	webSearchSecretModalSaving.value = true;
	webSearchSecretModalError.value = "";
	errorMessage.value = "";
	try {
		await callTool("auth_set", { name, value });
		await refreshCredentials();
		secretModalOnSave?.(name);
		closeWebSearchSecretModal();
		okMessage.value = `Secret stored: ${name}`;
	} catch (e) {
		const message = e instanceof Error ? e.message : String(e);
		webSearchSecretModalError.value = message;
		errorMessage.value = message;
	} finally {
		webSearchSecretModalSaving.value = false;
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
	errorMessage.value = "";
	okMessage.value = "";
	try {
		const parsed = await authStartOpenAI();
		okMessage.value = parsed.browser_opened
			? "OpenAI Codex authorization page opened. This callback stays available for 2 minutes."
			: parsed.browser_open_error ||
				"OpenAI Codex authorization started. This callback stays available for 2 minutes.";
	} catch (e) {
		errorMessage.value = e instanceof Error ? e.message : String(e);
	}
}

async function loginGemini() {
	errorMessage.value = "";
	okMessage.value = "";
	try {
		const parsed = await authStartGemini();
		okMessage.value = parsed.browser_opened
			? "Gemini authorization page opened. This callback stays available for 2 minutes."
			: parsed.browser_open_error ||
				"Gemini authorization started. This callback stays available for 2 minutes.";
	} catch (e) {
		errorMessage.value = e instanceof Error ? e.message : String(e);
	}
}

async function completeOpenAI() {
	errorMessage.value = "";
	okMessage.value = "";
	try {
		const text = await authCompleteOpenAI();
		clearOAuthState();
		providerAddSelection.value = "";
		okMessage.value = text || "OpenAI Codex OAuth completed.";
		await refreshCredentials();
	} catch (e) {
		errorMessage.value = e instanceof Error ? e.message : String(e);
	}
}

async function completeGemini() {
	errorMessage.value = "";
	okMessage.value = "";
	try {
		const text = await authCompleteGemini();
		clearOAuthState();
		providerAddSelection.value = "";
		okMessage.value = text || "Gemini OAuth completed.";
		await refreshCredentials();
	} catch (e) {
		errorMessage.value = e instanceof Error ? e.message : String(e);
	}
}

async function startCopilot() {
	errorMessage.value = "";
	okMessage.value = "";
	try {
		await authStartCopilot();
		okMessage.value =
			"GitHub Copilot step 2 is ready below. Open GitHub's device page, enter the one-time code there, then come back and click Complete.";
		await nextTick();
		document
			.getElementById("copilot-device-flow")
			?.scrollIntoView({ behavior: "smooth", block: "nearest" });
	} catch (e) {
		errorMessage.value = e instanceof Error ? e.message : String(e);
	}
}

async function completeCopilot() {
	errorMessage.value = "";
	okMessage.value = "";
	try {
		const text = await authCompleteCopilot();
		copilotUserCode.value = "";
		copilotVerifyUrl.value = "";
		providerAddSelection.value = "";
		okMessage.value = text || "GitHub Copilot login completed.";
		await refreshCredentials();
	} catch (e) {
		errorMessage.value = e instanceof Error ? e.message : String(e);
	}
}

async function startAnthropic() {
	errorMessage.value = "";
	okMessage.value = "";
	try {
		okMessage.value = await authStartAnthropic();
	} catch (e) {
		errorMessage.value = e instanceof Error ? e.message : String(e);
	}
}

async function completeAnthropic() {
	if (!anthropicCode.value.trim()) return;
	errorMessage.value = "";
	okMessage.value = "";
	try {
		const text = await authCompleteAnthropic();
		anthropicCode.value = "";
		anthropicUrl.value = "";
		providerAddSelection.value = "";
		okMessage.value = text || "Anthropic OAuth completed.";
		await refreshCredentials();
	} catch (e) {
		errorMessage.value = e instanceof Error ? e.message : String(e);
	}
}

const settingsContext = proxyRefs({
	activeTab,
	addAgent,
	addAllowFrom,
	addChannel,
	addProviderApiKey,
	addProviderOAuth,
	addSecret,
	addTask,
	agentExecAllowedCommands,
	agentFileEditorState,
	agentFilesystemAllowedPaths,
	agentInspectionTitle,
	agentPermissionsPreset,
	agentToolResolution,
	allowFromCardClass,
	anthropicCode,
	anthropicUrl,
	availableModelOptions,
	availableProviderOptions,
	availableToolNamesForAgent,
	availableToolsForAgent,
	browseSlackChannels,
	canDeleteAgentFile,
	cdpPortInput,
	channelCardClass,
	channelListButtonClass,
	channelInspectionTitle,
	channelPrimaryHelp,
	channelPrimaryLabel,
	channelPrimaryPlaceholder,
	channelTypeIconClass,
	channelTypeLabel,
	channelToolResolution,
	compileToastVisible,
	completeAnthropic,
	completeGemini,
	completeOpenAI,
	completeCopilot,
	concurrencyInput,
	configuredChannelLabel,
	configuredProviders,
	configuredTaskChannelOptions,
	convertTaskToScript,
	createAgentFile,
	createSession,
	createTaskFromName,
	deleteProviderCredential,
	deleteSecret,
	draft,
	entryExcludePrefixes,
	entryInspectionTitle,
	entryMentionPrefixes,
	entryToolResolution,
	execShellPlaceholder,
	extraSecrets,
	geminiCallbackUrl,
	geminiUrl,
	geminiRemainingSeconds,
	geminiTimedOut,
	formatDate,
	formatCountdown,
	getAgentFileState,
	hasEntryToolRestriction,
	hasToolRestriction,
	installedSkills,
	isAgentCategoryAccessible,
	isAgentToolAccessible,
	isAllowFromEnabled,
	isCategoryFullyEnabled,
	isCategoryPartiallyEnabled,
	isChannelEnabled,
	isEntryCategoryFullyEnabled,
	isEntryCategoryPartiallyEnabled,
	isEntryToolEnabled,
	isProtectedAgentFile,
	isTaskEnabled,
	isToolEnabled,
	loadAgentFiles,
	loadInstalledSkills,
	loadSessions,
	moveTaskToFile,
	newTaskNameMap,
	oauthBusy,
	openAICallbackUrl,
	openAIUrl,
	openAIRemainingSeconds,
	openAITimedOut,
	onAgentNameChange,
	onTaskNameBlur,
	onTaskNameFocus,
	openToolInspectionModal,
	PERMISSION_PRESET_OPTIONS,
	promptDeleteAgentFile,
	promptDeleteTask,
	providerAddSelection,
	providerApiKeyValue,
	reauthorizeProvider,
	refreshCredentials,
	removeAgent,
	removeAllowFrom,
	removeChannel,
	removeTarget,
	removeTargetOpen,
	removeTask,
	renamingTask,
	saveAgentFile,
	secretName,
	secretValue,
	selectAgentFile,
	selectedAgentAsSingletonList,
	selectedAgentIdx,
	selectedAgentSubtab,
	selectedChannel,
	selectedChannelIdx,
	selectedConfiguredChannel,
	selectedConfiguredChannelIndex,
	selectedTask,
	selectedTaskIdx,
	selectedTaskTarget,
	serverPortInput,
	sessionAgent,
	sessionLoading,
	sessions,
	setAgentExecAllowedCommands,
	setAgentExecShell,
	setAgentExecShellInterpolate,
	setAgentFilesystemAllowedPaths,
	setAllowFromEnabled,
	setChannelEnabled,
	setEntryExcludePrefixes,
	setEntryMentionPrefixes,
	setEntryToolRestriction,
	setSelectedTaskEnabled,
	setSelectedTaskTarget,
	setSkillArraySetting,
	setSkillStringSetting,
	setTaskChannelSelection,
	setToolRestriction,
	skillArraySetting,
	skillConfig,
	skillSettingEntries,
	skillSettingInputKind,
	skillSettingLabel,
	skillSettingPlaceholder,
	skillsLoading,
	skillStringSetting,
	slackChannelsForTask,
	slackTargetPlaceholder,
	slackVisibleChannels,
	slackWorkspaceState,
	startAnthropic,
	statusBadgeClass,
	stopSession,
	syncAgentTemplates,
	taskChannelSelection,
	taskDefinedIn,
	taskDeliveryTargetLabel,
	toggleCategory,
	toggleEntryCategory,
	toggleEntryTool,
	toggleTool,
	toolCategoryLabel,
	toolGroupEntries,
	updateAgentPermissionsPreset,
	updateCDPPortInput,
	updateServerPortInput,
	closeWebSearchSecretModal,
	openChannelTokenSecretModal,
	openWebSearchSecretModal,
	saveWebSearchSecret,
	webSearchSecretModalError,
	webSearchSecretModalName,
	webSearchSecretModalOpen,
	webSearchSecretModalSaving,
	webSearchSecretModalValue,
	webSearchSecretOptions,
	webSearchSecretRef,
	secretModalDescription,
	secretModalNamePlaceholder,
	secretModalTitle,
	secretModalValueLabel,
	secretModalValuePlaceholder,
});

provide(settingsViewContextKey, settingsContext);
</script>

<style scoped>
@reference "../style.css";

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
