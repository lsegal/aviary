<template>
	<AppLayout>
		<!-- One task: its runs and logs, with the session they share below. -->
		<template v-if="task && workflow" #header-icon>
			<BoltIcon class="h-4 w-4 text-gray-500 dark:text-gray-400" />
		</template>
		<template v-if="task" #header-status>
			<span v-if="enabled" data-testid="task-state" data-enabled="true"
				class="shrink-0 rounded-full bg-green-50 px-2 py-0.5 text-xs font-semibold text-green-700 dark:bg-green-950/50 dark:text-green-300">
				Enabled
			</span>
			<PausedChip v-else data-testid="task-state" data-enabled="false" />
			<CompiledStatusChip :task="task" :attempts="compileAttempts" detail :to="settingsLink"
				@click="compiledPanelOpen = true" />
		</template>
		<template v-if="task" #header>
			<p class="min-w-0 truncate" data-testid="task-schedule">
				{{ typeLabel }} · {{ scheduleText }}<template v-if="codeLoopTarget"> · sends to
					<CodeLoopTargetLink :target="codeLoopTarget" /></template>
			</p>
		</template>
		<template v-if="task" #header-actions>
			<div class="flex flex-wrap items-center gap-2" role="group" aria-label="Task actions">
				<button type="button" data-testid="task-run-now" :disabled="busy" :class="ACTION_CLS" @click="act(dash.runNow)">
					<PlayIcon aria-hidden="true" class="h-4 w-4" /> Run now
				</button>
				<button type="button" data-testid="task-toggle-enabled" :disabled="busy" :class="ACTION_CLS"
					@click="act(() => setEnabled(!enabled))">
					<component :is="enabled ? PauseIcon : PlayCircleIcon" aria-hidden="true" class="h-4 w-4" />
					{{ enabled ? "Pause" : "Resume" }}
				</button>
				<button v-if="precompiles" type="button" data-testid="task-recompile" :disabled="busy" :class="ACTION_CLS"
					title="Discard the compiled script and compile a fresh one" @click="act(recompile)">
					<ArrowPathIcon aria-hidden="true" class="h-4 w-4" /> Recompile
				</button>
				<button v-if="!workflow" type="button" data-testid="task-duplicate" :class="HEADER_COLLAPSE_CLS"
					aria-label="Duplicate" title="Open the create form prefilled from this task" @click="duplicate">
					<DocumentDuplicateIcon aria-hidden="true" :class="HEADER_COLLAPSE_ICON_CLS" />
					<span :class="HEADER_COLLAPSE_LABEL_CLS">Duplicate</span>
				</button>
				<button v-if="current?.job" type="button" data-testid="task-stop" :disabled="busy"
					:class="[ACTION_CLS, 'text-red-600! dark:text-red-400!']" @click="act(() => dash.stop(current!.id))">
					<StopIcon aria-hidden="true" class="h-4 w-4" /> Stop
				</button>
				<button type="button" data-testid="task-chat-toggle" :aria-pressed="chat.open.value"
					:aria-label="chat.open.value ? 'Hide chat' : 'Show chat'" :title="chat.open.value ? 'Hide chat' : 'Show chat'"
					:class="chat.open.value ? HEADER_ICON_ON_CLS : HEADER_ICON_CLS"
					@click="chat.open.value = !chat.open.value">
					<ChatBubbleLeftRightIcon aria-hidden="true" class="h-5 w-5" />
				</button>
				<router-link :to="settingsLink" data-testid="task-settings-link"
					:aria-label="workflow ? 'Workflow settings' : 'Task settings'" :title="workflow ? 'Workflow settings' : 'Task settings'"
					:class="HEADER_ICON_CLS">
					<Cog6ToothIcon aria-hidden="true" class="h-5 w-5" />
				</router-link>
			</div>
		</template>
		<div class="flex min-h-0 flex-1 flex-col">
			<template v-if="task">
				<div class="min-h-0 flex-1 overflow-y-auto" data-testid="task-dashboard">
					<div class="space-y-4 px-4 py-4 sm:px-6">
						<p v-if="dash.error.value" role="alert" data-testid="task-dashboard-error"
							class="rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700 dark:bg-red-950 dark:text-red-300">{{ dash.error.value }}</p>
						<p v-if="recompileError" role="alert" data-testid="task-recompile-error"
							class="rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700 dark:bg-red-950 dark:text-red-300">{{ recompileError }}</p>
						<div v-if="scriptFailing" role="status" data-testid="task-script-failing"
							class="flex flex-wrap items-center gap-2 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-800 dark:border-red-900 dark:bg-red-950/40 dark:text-red-300">
							<p class="min-w-0 flex-1">
								<span class="font-semibold">Script failing.</span>
								The compiled script failed its last {{ task.script_failures === 1 ? "run" : `${task.script_failures} runs` }}<template v-if="task.script_last_error"> (last error: <code data-testid="task-script-last-error">{{ task.script_last_error }}</code>)</template>, so each fell back to the model. Precompile stays on.
							</p>
							<button type="button" data-testid="task-script-failing-recompile" :disabled="busy"
								class="inline-flex shrink-0 items-center gap-1 rounded-md border border-red-300 bg-white px-2 py-1 text-xs font-semibold text-red-700 hover:bg-red-100 disabled:opacity-50 dark:border-red-800 dark:bg-red-950 dark:text-red-300 dark:hover:bg-red-900"
								@click="act(recompile)">
								<ArrowPathIcon aria-hidden="true" class="h-3.5 w-3.5" /> Recompile
							</button>
						</div>

						<router-view v-if="route.params.run" :dashboard="dash" />
						<template v-else>
							<StatChipRow aria-label="Run stats" :cards="statChips" @select="filter = $event as TaskRunFilter" />

							<TaskRunLog v-if="current" :entry="current" title="Current run" :now="dash.now.value"
								:fetch-logs="dash.fetchLogs" class="h-96" @stop="act(() => dash.stop($event))" />
							<TaskRunLog v-else-if="dash.stats.value.last" :entry="dash.stats.value.last" title="Last run"
								:now="dash.now.value" :fetch-logs="dash.fetchLogs" class="h-72" />
							<TriggerContextPanel v-if="shownRunContext" :context="shownRunContext" />
							<p v-if="dash.loaded.value && !current && !dash.stats.value.last" data-testid="task-run-log-empty"
								class="rounded-xl border border-dashed border-gray-300 px-4 py-6 text-center text-sm text-gray-500 dark:border-gray-700 dark:text-gray-400">
								{{ workflow ? "This workflow hasn't run yet. A matching message starts a run; its logs show here while it runs, and the last run's after." : "This task hasn't run yet. Its current run's logs show here while it runs, and the last run's after." }}
							</p>

							<TaskRunHistory v-model:filter="filter" :entries="dash.entries.value" :agent="agent" :task="task.name"
								:now="dash.now.value" />
						</template>
					</div>
				</div>

				<!-- The task's session: a resizable bottom panel, or a bottom sheet on
					narrow screens. -->
				<div v-if="chat.open.value && !chat.desktop.value" class="fixed inset-0 z-40 bg-black/40"
					data-testid="task-chat-backdrop" @click="chat.open.value = false" />
				<section v-if="chat.open.value" data-testid="task-chat-panel" aria-label="Task chat"
					:data-layout="chat.desktop.value ? 'panel' : 'sheet'"
					class="flex flex-col bg-white dark:bg-gray-950"
					:class="chat.desktop.value ? 'relative shrink-0 border-t border-gray-200 dark:border-gray-800' : 'fixed inset-x-0 bottom-0 z-50 h-[85dvh] rounded-t-2xl shadow-2xl'"
					:style="chat.desktop.value ? { height: `${chat.height.value}px` } : undefined">
					<div v-if="chat.desktop.value" data-testid="task-chat-resize" role="separator" tabindex="0"
						aria-orientation="horizontal" aria-label="Resize chat" :aria-valuenow="chat.height.value"
						:aria-valuemin="TASK_CHAT_MIN_HEIGHT" :aria-valuemax="chat.maxHeight.value"
						class="absolute inset-x-0 -top-1 z-20 h-2 cursor-row-resize hover:bg-blue-500/30 focus:bg-blue-500/30 focus:outline-none"
						@pointerdown="chat.startResize" @keydown="chat.resizeByKey" />
					<header v-else class="flex items-center gap-2 border-b border-gray-200 px-4 py-2 dark:border-gray-800">
						<h3 class="flex-1 text-sm font-semibold text-gray-900 dark:text-white">Chat</h3>
						<button type="button" aria-label="Close chat" title="Close chat" data-testid="task-chat-close"
							class="rounded-lg px-2 py-0.5 text-gray-500 hover:bg-gray-100 hover:text-gray-800 dark:hover:bg-gray-800 dark:hover:text-gray-200"
							@click="chat.open.value = false">✕</button>
					</header>
					<div class="min-h-0 flex-1">
						<ChatView v-if="sessionId" :key="sessionId" :agent="agent" :session="sessionId" data-testid="task-chat" />
						<p v-else-if="error" role="alert" class="px-6 py-4 text-sm text-red-600 dark:text-red-400">{{ error }}</p>
					</div>
				</section>
			</template>
			<div v-else class="flex h-full flex-col items-center justify-center gap-3 px-6 text-center text-sm text-gray-500 dark:text-gray-400"
				data-testid="task-missing">
				<p>No task named <span class="font-medium text-gray-900 dark:text-white">{{ taskParam }}</span>.</p>
				<router-link :to="agentTaskPath(agent)"
					class="rounded-lg border border-gray-200 px-3 py-1.5 text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800">
					All scheduled tasks
				</router-link>
			</div>
		</div>
	</AppLayout>
</template>

<script setup lang="ts">
import ArrowPathIcon from "@heroicons/vue/24/outline/ArrowPathIcon";
import BoltIcon from "@heroicons/vue/24/outline/BoltIcon";
import ChatBubbleLeftRightIcon from "@heroicons/vue/24/outline/ChatBubbleLeftRightIcon";
import Cog6ToothIcon from "@heroicons/vue/24/outline/Cog6ToothIcon";
import DocumentDuplicateIcon from "@heroicons/vue/24/outline/DocumentDuplicateIcon";
import PauseIcon from "@heroicons/vue/24/outline/PauseIcon";
import PlayCircleIcon from "@heroicons/vue/24/outline/PlayCircleIcon";
import PlayIcon from "@heroicons/vue/24/outline/PlayIcon";
import StopIcon from "@heroicons/vue/24/outline/StopIcon";
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useRoute } from "vue-router";
import AppLayout from "../components/AppLayout.vue";
import CodeLoopTargetLink from "../components/CodeLoopTargetLink.vue";
import StatChipRow, {
	type StatChip,
} from "../components/projects/StatChipRow.vue";
import CompiledStatusChip from "../components/tasks/CompiledStatusChip.vue";
import TaskRunHistory, {
	type TaskRunFilter,
} from "../components/tasks/dashboard/TaskRunHistory.vue";
import TaskRunLog from "../components/tasks/dashboard/TaskRunLog.vue";
import TriggerContextPanel from "../components/tasks/dashboard/TriggerContextPanel.vue";
import PausedChip from "../components/tasks/PausedChip.vue";
import { useAgentItemDialogs } from "../composables/useAgentItemDialogs";
import { useMCP } from "../composables/useMCP";
import {
	TASK_CHAT_MIN_HEIGHT,
	useTaskChatPanel,
} from "../composables/useTaskChatPanel";
import { useCompiledPanelOpen } from "../composables/useTaskCompiles";
import { useTaskDashboard } from "../composables/useTaskDashboard";
import { useTaskPause } from "../composables/useTaskPause";
import { workflowSettingsPath } from "../lib/channels";
import {
	compiledStatus,
	isScriptFailing,
	type TaskCompileAttempt,
} from "../lib/compiledStatus";
import {
	HEADER_ACTION_CLS,
	HEADER_COLLAPSE_CLS,
	HEADER_COLLAPSE_ICON_CLS,
	HEADER_COLLAPSE_LABEL_CLS,
	HEADER_ICON_CLS,
	HEADER_ICON_ON_CLS,
} from "../lib/headerActions";
import { age, until } from "../lib/projects";
import { RESULT_LABELS } from "../lib/taskRuns";
import { isCodeLoopTarget } from "../lib/taskTargets";
import { formatDuration } from "../stores/jobs";
import {
	agentTaskPath,
	agentTaskType,
	isWorkflow,
	precompilesBesidePrompt,
	useSettingsStore,
} from "../stores/settings";
import ChatView from "./ChatView.vue";

const route = useRoute();
const settings = useSettingsStore();
const { callTool } = useMCP();
const chat = useTaskChatPanel();

const agent = computed(() =>
	typeof route.params.agent === "string" ? route.params.agent : "",
);
const taskParam = computed(() =>
	typeof route.params.task === "string" ? route.params.task : "",
);

const task = computed(() =>
	settings.config?.agents
		.find((a) => a.name === agent.value)
		?.tasks.find((t) => t.name === taskParam.value),
);

const TYPE_LABELS = {
	prompt: "Prompt task",
	script: "Script task",
	project: "Project task",
} as const;
const workflow = computed(() => !!task.value && isWorkflow(task.value));
const typeLabel = computed(() =>
	workflow.value ? "Workflow" : TYPE_LABELS[agentTaskType(task.value?.type)],
);
// A workflow is edited in its channel's Workflows tab, a task on Tasks.
const settingsLink = computed(() =>
	workflow.value && task.value?.channel
		? workflowSettingsPath(agent.value, task.value.channel, task.value.name)
		: agentTaskPath(agent.value, task.value?.name ?? ""),
);
/**
 * Whether the task's runs can try a compiled script and fall back to the
 * model: a project task or prompt workflow that has precompile on.
 */
const precompiles = computed(
	() =>
		!!task.value &&
		precompilesBesidePrompt(task.value) &&
		task.value.precompile === true,
);

// Whether the compiled script failed its last runs, which fell back to the model.
const scriptFailing = computed(
	() => !!task.value && isScriptFailing(task.value),
);

// The header's compiled chip opens the editor with the compiled script shown.
const compiledPanelOpen = useCompiledPanelOpen();

// How many times the task's script was compiled, and how the latest went.
const compileAttempts = ref<TaskCompileAttempt[]>([]);
async function loadCompileAttempts() {
	const name = taskParam.value;
	if (!precompiles.value) {
		compileAttempts.value = [];
		return;
	}
	try {
		const raw = await callTool("task_compile_query", {
			agent: agent.value,
			task: name,
		});
		const list = (JSON.parse(raw) ?? []) as TaskCompileAttempt[];
		if (name !== taskParam.value) return;
		compileAttempts.value = list.sort((a, b) =>
			b.created_at.localeCompare(a.created_at),
		);
	} catch {
		compileAttempts.value = [];
	}
}
watch(
	() => [
		agent.value,
		taskParam.value,
		precompiles.value,
		task.value?.compiled_key,
	],
	() => void loadCompileAttempts(),
	{ immediate: true },
);
// A Code Loop target links to its job from the header.
const codeLoopTarget = computed(() =>
	isCodeLoopTarget(task.value?.target) ? (task.value?.target ?? "") : "",
);

// ── Runs ────────────────────────────────────────────────────────────────────

const dash = useTaskDashboard(agent, taskParam);
const { setTaskEnabled } = useTaskPause();
const current = dash.current;
const enabled = computed(() => task.value?.enabled !== false);
const nextRun = computed(() => {
	const next = dash.info.value?.next_run;
	return enabled.value && next && Date.parse(next) > dash.now.value ? next : "";
});

const scheduleText = computed(() => {
	let text = dash.info.value?.description || "";
	if (!text && task.value?.watch) text = `when ${task.value.watch} changes`;
	if (!text) text = dash.loaded.value ? "manual only" : "…";
	if (!enabled.value) return `${text} · paused`;
	return nextRun.value
		? `${text} · next ${until(nextRun.value, dash.now.value)}`
		: text;
});

/** The trigger context of the run the dashboard shows: the current one, else the last. */
const shownRunContext = computed(
	() => (current.value ?? dash.stats.value.last)?.job?.trigger_context ?? null,
);

const filter = ref<TaskRunFilter>("all");
watch(taskParam, () => {
	filter.value = "all";
});

const CHIP_TONES = {
	queued:
		"border-violet-200 bg-violet-50 text-violet-800 dark:border-violet-900 dark:bg-violet-950/40 dark:text-violet-300",
	running:
		"border-blue-200 bg-blue-50 text-blue-800 dark:border-blue-900 dark:bg-blue-950/40 dark:text-blue-300",
	fallback:
		"border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-300",
	succeeded:
		"border-green-200 bg-green-50 text-green-800 dark:border-green-900 dark:bg-green-950/40 dark:text-green-300",
	failed:
		"border-red-200 bg-red-50 text-red-800 dark:border-red-900 dark:bg-red-950/40 dark:text-red-300",
	neutral: "border-gray-200 text-gray-900 dark:border-gray-800 dark:text-white",
};

const statChips = computed((): StatChip[] => {
	const s = dash.stats.value;
	const count = (
		key: "queued" | "running" | "succeeded" | "fallback" | "failed",
	): StatChip => ({
		label: RESULT_LABELS[key],
		value: s[key],
		chip: String(s[key]),
		chipLabel:
			key === "fallback" ? "via fallback" : RESULT_LABELS[key].toLowerCase(),
		cls: CHIP_TONES[key],
		filter: key,
		active: filter.value === key,
	});
	const rate = s.successRate === null ? "—" : `${s.successRate}%`;
	const last = s.last;
	const lastText = last
		? `${age(last.startedAt, dash.now.value)} ago · ${RESULT_LABELS[last.result].toLowerCase()}`
		: "—";
	const avg = s.avgDurationMs === null ? "—" : formatDuration(s.avgDurationMs);
	const next = nextRun.value ? until(nextRun.value, dash.now.value) : "—";
	return [
		count("queued"),
		count("running"),
		count("succeeded"),
		// Only a precompiled task's script can fail into the fallback.
		...(precompiles.value
			? [
					{
						label: "Compile attempts",
						value: compileAttempts.value.length,
						chip: String(compileAttempts.value.length),
						chipLabel: compileAttempts.value[0]
							? `compile attempts · last ${compileAttempts.value[0].status}`
							: "compile attempts",
						cls: CHIP_TONES.neutral,
					},
					count("fallback"),
					{
						label: "Script failures",
						value: s.scriptFailures,
						chip: String(s.scriptFailures),
						chipLabel: "script failures",
						cls: s.scriptFailures ? CHIP_TONES.fallback : CHIP_TONES.neutral,
					},
				]
			: []),
		count("failed"),
		{
			label: "Success rate",
			value: rate,
			chip: rate,
			chipLabel: "success",
			cls: CHIP_TONES.neutral,
			filter: "all",
			active: filter.value === "all",
		},
		{
			label: "Last run",
			value: lastText,
			chip: lastText,
			chipLabel: "last run",
			cls: CHIP_TONES.neutral,
			filter: last?.result,
			active: false,
		},
		{
			label: "Next run",
			value: next,
			chip: next,
			chipLabel: "next run",
			cls: CHIP_TONES.neutral,
		},
		{
			label: "Average duration",
			value: avg,
			chip: avg,
			chipLabel: "avg",
			cls: CHIP_TONES.neutral,
		},
	];
});

// ── Actions ─────────────────────────────────────────────────────────────────

const ACTION_CLS = HEADER_ACTION_CLS;

const busy = ref(false);
async function act(fn: () => Promise<unknown>) {
	busy.value = true;
	try {
		await fn();
	} finally {
		busy.value = false;
	}
}

// Duplicate opens the create form prefilled from this task (#1465).
const { open: openItemDialog } = useAgentItemDialogs();
function duplicate() {
	openItemDialog({
		kind: "task",
		agent: agent.value,
		mode: "create",
		duplicate: taskParam.value,
	});
}

async function setEnabled(value: boolean) {
	if (await setTaskEnabled(agent.value, taskParam.value, value))
		await dash.refresh();
}

// Recompile discards the task's script, whatever state it is in, turns
// precompile back on and compiles a fresh one in the background; the config
// is reloaded until that compile lands so the badge follows it.
const recompileError = ref("");
let recompilePoll: ReturnType<typeof setTimeout> | undefined;
async function recompile() {
	recompileError.value = "";
	clearTimeout(recompilePoll);
	try {
		await callTool("config_task_convert_to_script", {
			agent: agent.value,
			task: taskParam.value,
		});
	} catch (e) {
		recompileError.value = e instanceof Error ? e.message : String(e);
		return;
	}
	await settings.fetchConfig();
	let polls = 0;
	const poll = async () => {
		if (!task.value || compiledStatus(task.value)?.state !== "compiling")
			return;
		if (++polls > 60) return;
		recompilePoll = setTimeout(async () => {
			await settings.fetchConfig();
			await poll();
		}, 2000);
	};
	await poll();
}
onBeforeUnmount(() => clearTimeout(recompilePoll));

// ── Chat ────────────────────────────────────────────────────────────────────

const sessionId = ref("");
const error = ref("");

// The task's session is named after it; a rename moves the session along.
async function loadSession() {
	sessionId.value = "";
	error.value = "";
	const t = task.value;
	if (!t) return;
	try {
		const raw = await callTool("task_session", {
			agent: agent.value,
			name: t.name,
		});
		sessionId.value = (JSON.parse(raw) as { id: string }).id;
	} catch (e) {
		error.value = e instanceof Error ? e.message : String(e);
	}
}

watch(
	() => [agent.value, task.value?.name],
	() => void loadSession(),
	{ immediate: true },
);
</script>

