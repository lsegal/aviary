<template>
	<AppLayout>
		<div class="flex h-full min-h-0 flex-col">
			<!-- One task: its runs and logs, with the session they share below. -->
			<header v-if="task" data-testid="task-header"
				class="flex flex-wrap items-center gap-3 border-b border-gray-200 px-4 py-3 sm:px-6 dark:border-gray-800">
				<span class="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-300">
					<BoltIcon v-if="workflow" aria-hidden="true" class="h-4 w-4" />
					<ClockIcon v-else aria-hidden="true" class="h-4 w-4" />
				</span>
				<div class="min-w-0">
					<div class="flex items-center gap-2">
						<h2 class="truncate text-lg font-bold text-gray-900 dark:text-white" data-testid="task-title">{{ task.name }}</h2>
						<span v-if="enabled" data-testid="task-state" data-enabled="true"
							class="shrink-0 rounded-full bg-green-50 px-2 py-0.5 text-xs font-semibold text-green-700 dark:bg-green-950/50 dark:text-green-300">
							Enabled
						</span>
						<PausedChip v-else data-testid="task-state" data-enabled="false" />
						<TaskPrecompileBadge :task="task" />
					</div>
					<p class="truncate text-xs text-gray-500 dark:text-gray-400" data-testid="task-schedule">
						{{ typeLabel }} · {{ scheduleText }}
					</p>
				</div>
				<div class="ml-auto flex flex-wrap items-center gap-2" role="group" aria-label="Task actions">
					<button type="button" data-testid="task-run-now" :disabled="busy" :class="ACTION_CLS" @click="act(dash.runNow)">
						<PlayIcon aria-hidden="true" class="h-4 w-4" /> Run now
					</button>
					<button type="button" data-testid="task-toggle-enabled" :disabled="busy" :class="ACTION_CLS"
						@click="act(() => setEnabled(!enabled))">
						<component :is="enabled ? PauseIcon : PlayCircleIcon" aria-hidden="true" class="h-4 w-4" />
						{{ enabled ? "Pause" : "Resume" }}
					</button>
					<button v-if="current?.job" type="button" data-testid="task-stop" :disabled="busy"
						:class="[ACTION_CLS, 'text-red-600! dark:text-red-400!']" @click="act(() => dash.stop(current!.id))">
						<StopIcon aria-hidden="true" class="h-4 w-4" /> Stop
					</button>
					<button type="button" data-testid="task-chat-toggle" :aria-pressed="chat.open.value"
						:aria-label="chat.open.value ? 'Hide chat' : 'Show chat'" :title="chat.open.value ? 'Hide chat' : 'Show chat'"
						class="rounded-lg border p-1.5"
						:class="chat.open.value ? 'border-blue-600 bg-blue-50 text-blue-700 dark:border-blue-500 dark:bg-blue-950/50 dark:text-blue-300' : 'border-gray-200 text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800'"
						@click="chat.open.value = !chat.open.value">
						<ChatBubbleLeftRightIcon aria-hidden="true" class="h-5 w-5" />
					</button>
					<router-link :to="settingsLink" data-testid="task-settings-link"
						:aria-label="workflow ? 'Workflow settings' : 'Task settings'" :title="workflow ? 'Workflow settings' : 'Task settings'"
						class="rounded-lg border border-gray-200 p-1.5 text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800">
						<Cog6ToothIcon aria-hidden="true" class="h-5 w-5" />
					</router-link>
				</div>
			</header>

			<template v-if="task">
				<div class="min-h-0 flex-1 overflow-y-auto" data-testid="task-dashboard">
					<div class="space-y-4 px-4 py-4 sm:px-6">
						<p v-if="dash.error.value" role="alert" data-testid="task-dashboard-error"
							class="rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700 dark:bg-red-950 dark:text-red-300">{{ dash.error.value }}</p>
						<p v-if="task.precompile_note" role="status" data-testid="task-precompile-note"
							class="rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-800 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-300">
							{{ task.precompile_note }}
						</p>

						<router-view v-if="route.params.run" :dashboard="dash" :agent="agent" :task="task.name" />
						<template v-else>
							<StatChipRow aria-label="Run stats" :cards="statChips" @select="filter = $event as TaskRunFilter" />

							<TaskRunLog v-if="current" :entry="current" title="Current run" :now="dash.now.value"
								:fetch-logs="dash.fetchLogs" class="h-96" @stop="act(() => dash.stop($event))" />
							<TaskRunLog v-else-if="dash.stats.value.last" :entry="dash.stats.value.last" title="Last run"
								:now="dash.now.value" :fetch-logs="dash.fetchLogs" class="h-72" />
							<TriggerContextPanel v-if="shownRunContext" :context="shownRunContext" />
							<p v-else-if="dash.loaded.value" data-testid="task-run-log-empty"
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
import BoltIcon from "@heroicons/vue/24/outline/BoltIcon";
import ChatBubbleLeftRightIcon from "@heroicons/vue/24/outline/ChatBubbleLeftRightIcon";
import ClockIcon from "@heroicons/vue/24/outline/ClockIcon";
import Cog6ToothIcon from "@heroicons/vue/24/outline/Cog6ToothIcon";
import PauseIcon from "@heroicons/vue/24/outline/PauseIcon";
import PlayCircleIcon from "@heroicons/vue/24/outline/PlayCircleIcon";
import PlayIcon from "@heroicons/vue/24/outline/PlayIcon";
import StopIcon from "@heroicons/vue/24/outline/StopIcon";
import { computed, ref, watch } from "vue";
import { useRoute } from "vue-router";
import AppLayout from "../components/AppLayout.vue";
import StatChipRow, {
	type StatChip,
} from "../components/projects/StatChipRow.vue";
import TaskRunHistory, {
	type TaskRunFilter,
} from "../components/tasks/dashboard/TaskRunHistory.vue";
import TaskRunLog from "../components/tasks/dashboard/TaskRunLog.vue";
import TriggerContextPanel from "../components/tasks/dashboard/TriggerContextPanel.vue";
import PausedChip from "../components/tasks/PausedChip.vue";
import TaskPrecompileBadge from "../components/tasks/TaskPrecompileBadge.vue";
import { useMCP } from "../composables/useMCP";
import {
	TASK_CHAT_MIN_HEIGHT,
	useTaskChatPanel,
} from "../composables/useTaskChatPanel";
import { useTaskDashboard } from "../composables/useTaskDashboard";
import { useTaskPause } from "../composables/useTaskPause";
import { workflowSettingsPath } from "../lib/channels";
import { age, until } from "../lib/projects";
import { RESULT_LABELS } from "../lib/taskRuns";
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
 * model: a project task or prompt workflow that has precompile on, or had it
 * until Aviary turned it off.
 */
const precompiles = computed(
	() =>
		!!task.value &&
		precompilesBesidePrompt(task.value) &&
		(task.value.precompile === true || !!task.value.precompile_note),
);

// How many times the task's script was compiled, and how the latest went.
const compileAttempts = ref<{ status: string }[]>([]);
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
		const list = (JSON.parse(raw) ?? []) as {
			status: string;
			created_at: string;
		}[];
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

const ACTION_CLS =
	"inline-flex items-center gap-1 rounded-lg border border-gray-200 px-2.5 py-1 text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800";

const busy = ref(false);
async function act(fn: () => Promise<unknown>) {
	busy.value = true;
	try {
		await fn();
	} finally {
		busy.value = false;
	}
}

async function setEnabled(value: boolean) {
	if (await setTaskEnabled(agent.value, taskParam.value, value))
		await dash.refresh();
}

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

