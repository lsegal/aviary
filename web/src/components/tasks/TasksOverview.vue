<template>
	<div class="space-y-4" data-testid="tasks-overview">
		<!-- Summary: the agent's runs over the chosen range, and what's coming up. -->
		<section aria-label="Task run summary" class="space-y-3">
			<div class="flex flex-wrap items-center gap-2 text-sm">
				<div role="group" aria-label="Date range" class="flex overflow-hidden rounded-lg border border-gray-200 dark:border-gray-700">
					<button v-for="p in PRESETS" :key="p.days" type="button" :aria-pressed="activePreset === p.days"
						class="px-3 py-1.5 transition-colors"
						:class="activePreset === p.days ? 'bg-blue-600 text-white' : 'bg-white text-gray-600 hover:bg-gray-100 dark:bg-gray-900 dark:text-gray-400 dark:hover:bg-gray-800'"
						@click="applyPreset(p.days)">
						{{ p.label }}
					</button>
				</div>
				<div class="flex items-center gap-1">
					<input v-model="jobs.startDate" type="date" aria-label="Start date"
						class="rounded-lg border border-gray-200 bg-white px-2 py-1.5 text-gray-700 dark:border-gray-700 dark:bg-gray-900 dark:text-gray-300"
						@change="onDatesChanged" />
					<span class="text-gray-400">to</span>
					<input v-model="jobs.endDate" type="date" aria-label="End date"
						class="rounded-lg border border-gray-200 bg-white px-2 py-1.5 text-gray-700 dark:border-gray-700 dark:bg-gray-900 dark:text-gray-300"
						@change="onDatesChanged" />
				</div>
			</div>

			<div class="grid grid-cols-2 gap-3 sm:grid-cols-4 xl:grid-cols-7" data-testid="task-stats">
				<div class="col-span-2 rounded-xl border border-gray-200 px-4 py-3 text-gray-900 sm:col-span-2 dark:border-gray-800 dark:text-white" data-testid="stat-runs">
					<div class="flex items-baseline justify-between gap-2">
						<p class="text-xs font-semibold uppercase tracking-wide opacity-80">Runs</p>
						<p class="text-[10px] text-gray-500">
							<span class="text-green-600 dark:text-green-400">{{ jobs.completed.length }} ok</span> ·
							<span class="text-red-600 dark:text-red-400">{{ jobs.failed.length }} failed</span>
						</p>
					</div>
					<p class="text-2xl font-bold">{{ jobs.jobs.length }}</p>
					<div class="mt-1 flex h-8 items-end gap-px" data-testid="runs-sparkline" aria-hidden="true">
						<div v-for="row in jobs.byDay" :key="row.date" class="flex h-full flex-1 flex-col justify-end"
							:title="`${row.date}: ${row.completed} ok, ${row.failed} failed`">
							<div v-if="row.failed" class="w-full bg-red-400 dark:bg-red-600" :style="{ height: sparkH(row.failed) }" />
							<div v-if="row.completed" class="w-full bg-green-500 dark:bg-green-600" :style="{ height: sparkH(row.completed) }" />
						</div>
					</div>
				</div>
				<StatCard v-for="s in statCards" :key="s.label" :label="s.label" :value="s.value" :caption="s.caption"
					:pulse="s.pulse" :tone="s.tone" :data-testid="s.testid" />
			</div>

			<div>
				<button type="button" data-testid="more-analytics-toggle" :aria-expanded="analyticsOpen"
					class="inline-flex items-center gap-1 text-sm font-medium text-blue-600 hover:underline dark:text-blue-400"
					@click="analyticsOpen = !analyticsOpen">
					<ChevronRightIcon aria-hidden="true" class="h-4 w-4 transition-transform" :class="analyticsOpen ? 'rotate-90' : ''" />
					More analytics
				</button>
				<p v-if="jobs.error" class="mt-2 rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700 dark:bg-red-950 dark:text-red-300">{{ jobs.error }}</p>
				<TaskAnalytics v-if="analyticsOpen" v-model:task="runsTask" class="mt-3" />
			</div>
		</section>

		<!-- The tasks: cards or a dense list. -->
		<section aria-label="Scheduled tasks" class="space-y-3">
			<div class="flex flex-wrap items-center gap-3 text-sm" data-testid="task-toolbar">
				<div class="relative min-w-0 flex-1 sm:max-w-xs">
					<span class="pointer-events-none absolute inset-y-0 left-3 flex items-center text-gray-400 dark:text-gray-500" aria-hidden="true">
						<MagnifyingGlassIcon class="h-4 w-4" />
					</span>
					<input v-model="search" type="search" aria-label="Search tasks" placeholder="Search tasks…" class="field-input pl-9!" />
				</div>
				<div role="group" aria-label="Task filter" class="flex flex-wrap items-center gap-1.5">
					<button v-for="f in FILTERS" :key="f.key" type="button" :aria-pressed="filter === f.key"
						class="shrink-0 rounded-full border px-3 py-1"
						:class="filter === f.key ? 'border-blue-600 bg-blue-600 text-white' : 'border-gray-300 text-gray-600 hover:bg-gray-50 dark:border-gray-600 dark:text-gray-300 dark:hover:bg-gray-800'"
						@click="filter = f.key">
						{{ f.label }}<span class="ml-1.5 tabular-nums opacity-75">{{ filterCount(f.key) }}</span>
					</button>
				</div>
				<div role="group" aria-label="Task layout" class="flex items-center gap-1 sm:ml-auto">
					<button v-for="l in LAYOUTS" :key="l.key" type="button" :data-testid="`layout-${l.key}`" :aria-pressed="layout === l.key"
						:aria-label="l.label" :title="l.label" class="rounded-lg border p-1.5"
						:class="layout === l.key ? 'border-blue-600 bg-blue-600 text-white' : 'border-gray-200 text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800'"
						@click="layout = l.key">
						<component :is="l.icon" aria-hidden="true" class="h-5 w-5" />
					</button>
				</div>
			</div>

			<p v-if="actionError" class="rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700 dark:bg-red-950 dark:text-red-300">{{ actionError }}</p>

			<EmptyStateCard v-if="!rows.length">
				<template v-if="tasks.length">No tasks match.</template>
				<template v-else>No scheduled tasks configured for this agent. Add one with + New task.</template>
			</EmptyStateCard>

			<div v-else-if="layout === 'cards'" class="grid gap-3 sm:grid-cols-2 xl:grid-cols-3" data-testid="task-cards">
				<!-- The whole card opens the task's page, the same one its sidebar row
					opens: its name is a link stretched over the card, and the controls
					sit above it. -->
				<article v-for="r in rows" :key="r.task.name" data-testid="task-card" :data-task="r.task.name"
					class="relative cursor-pointer space-y-1.5 rounded-xl border border-gray-200 bg-white px-3 py-2.5 focus-within:ring-2 focus-within:ring-blue-500 hover:border-blue-300 hover:shadow-sm dark:border-gray-800 dark:bg-gray-900 dark:hover:border-blue-700"
					:class="r.enabled ? '' : 'opacity-75'">
					<div class="flex items-center gap-2">
						<span v-if="r.running" data-testid="task-running" title="Running now" class="inline-block h-2 w-2 shrink-0 animate-pulse rounded-full bg-blue-500" />
						<h3 class="min-w-0 flex-1 truncate font-mono text-sm font-semibold text-gray-900 dark:text-white">
							<router-link :to="taskSessionPath(agent, r.task.name)" data-testid="task-card-link"
								class="after:absolute after:inset-0 after:rounded-xl focus:outline-none">{{ r.task.name }}</router-link>
						</h3>
						<TaskEnabledSwitch class="z-10" :task="r.task.name" :enabled="r.enabled" :busy="busy === r.id || isPending(agent, r.task.name)" @update="setEnabled(r, $event)" />
					</div>
					<div class="flex min-w-0 items-center gap-2">
						<PausedChip v-if="!r.enabled" />
						<CompiledStatusChip :task="r.task" />
						<p class="min-w-0 truncate text-xs text-gray-500 dark:text-gray-400" data-testid="task-owner">{{ r.owner }}</p>
					</div>
					<p class="truncate text-xs text-gray-700 dark:text-gray-300" data-testid="task-schedule" :title="r.schedule">{{ r.schedule }}</p>
					<div class="flex items-center justify-between gap-2">
						<p class="min-w-0 truncate text-xs" data-testid="task-last-run">
							<router-link v-if="r.running" :to="taskSessionPath(agent, r.task.name)" data-testid="task-running-link"
								title="Open the current run" class="relative z-10 font-medium text-blue-600 hover:underline dark:text-blue-400">Running now</router-link>
							<template v-else-if="r.last">
								<span class="font-medium" :class="RUN_CLASS[r.last.status]">{{ RUN_LABEL[r.last.status] }}</span>
								<span class="text-gray-500 dark:text-gray-400">{{ ` ${ago(r.last.created_at)}` }}</span>
							</template>
							<span v-else class="text-gray-400">Never run</span>
						</p>
						<TaskActions class="relative z-10" :row="r" :busy="busy === r.id" @action="onAction(r, $event)" />
					</div>
				</article>
			</div>

			<div v-else class="overflow-x-auto rounded-xl border border-gray-200 bg-white dark:border-gray-800 dark:bg-gray-900" data-testid="task-list">
				<table class="w-full text-left text-xs">
					<thead>
						<tr class="border-b border-gray-100 text-gray-400 dark:border-gray-800">
							<th class="px-4 py-2 font-medium">Task</th>
							<th class="px-3 py-2 font-medium">Enabled</th>
							<th class="px-3 py-2 font-medium">Owner</th>
							<th class="px-3 py-2 font-medium">Schedule</th>
							<th class="px-3 py-2 font-medium">Last run</th>
							<th class="px-3 py-2 text-right font-medium">Actions</th>
						</tr>
					</thead>
					<tbody>
						<tr v-for="r in rows" :key="r.task.name" data-testid="task-row-item" :data-task="r.task.name"
							class="cursor-pointer border-b border-gray-50 text-gray-700 hover:bg-gray-50 dark:border-gray-800 dark:text-gray-300 dark:hover:bg-gray-800/50"
							@click="openTask(r.task.name)">
							<td class="px-4 py-2">
								<span class="inline-flex items-center gap-1.5 font-mono font-medium">
									<span v-if="r.running" data-testid="task-running" title="Running now" class="inline-block h-1.5 w-1.5 animate-pulse rounded-full bg-blue-500" />
									<router-link :to="taskSessionPath(agent, r.task.name)" class="hover:underline" @click.stop>{{ r.task.name }}</router-link>
									<PausedChip v-if="!r.enabled" />
									<CompiledStatusChip :task="r.task" />
								</span>
							</td>
							<td class="px-3 py-2"><TaskEnabledSwitch :task="r.task.name" :enabled="r.enabled" :busy="busy === r.id || isPending(agent, r.task.name)" @update="setEnabled(r, $event)" /></td>
							<td class="px-3 py-2 text-gray-500 dark:text-gray-400">{{ r.owner }}</td>
							<td class="max-w-xs truncate px-3 py-2" :title="r.schedule">{{ r.schedule }}</td>
							<td class="px-3 py-2">
								<span v-if="r.running" class="font-medium text-blue-600 dark:text-blue-400">Running now</span>
								<template v-else-if="r.last">
									<span class="font-medium" :class="RUN_CLASS[r.last.status]">{{ RUN_LABEL[r.last.status] }}</span>
									<span class="text-gray-500 dark:text-gray-400">{{ ` ${ago(r.last.created_at)}` }}</span>
								</template>
								<span v-else class="text-gray-400">Never run</span>
							</td>
							<td class="px-3 py-2 text-right">
								<TaskActions :row="r" :busy="busy === r.id" @action="onAction(r, $event)" />
							</td>
						</tr>
					</tbody>
				</table>
			</div>
		</section>
	</div>
</template>

<script setup lang="ts">
import ChevronRightIcon from "@heroicons/vue/24/outline/ChevronRightIcon";
import ListBulletIcon from "@heroicons/vue/24/outline/ListBulletIcon";
import MagnifyingGlassIcon from "@heroicons/vue/24/outline/MagnifyingGlassIcon";
import Squares2X2Icon from "@heroicons/vue/24/outline/Squares2X2Icon";
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { useRouter } from "vue-router";
import { useAgentItemDialogs } from "../../composables/useAgentItemDialogs";
import { useMCP } from "../../composables/useMCP";
import { useTaskPause } from "../../composables/useTaskPause";
import { age, parseToolJSON, until } from "../../lib/projects";
import {
	formatDuration,
	type Job,
	jobDurationMs,
	useJobsStore,
} from "../../stores/jobs";
import {
	type AgentTask,
	isWorkflow,
	taskSessionPath,
	useSettingsStore,
} from "../../stores/settings";
import EmptyStateCard from "../ui/EmptyStateCard.vue";
import StatCard from "../ui/StatCard.vue";
import CompiledStatusChip from "./CompiledStatusChip.vue";
import PausedChip from "./PausedChip.vue";
import TaskActions from "./TaskActions.vue";
import TaskAnalytics from "./TaskAnalytics.vue";
import TaskEnabledSwitch from "./TaskEnabledSwitch.vue";
import type { TaskAction, TaskRow } from "./types";

const props = defineProps<{ agent: string }>();

const router = useRouter();
const settings = useSettingsStore();
const jobs = useJobsStore();
const { callTool } = useMCP();
const { setTaskEnabled, isPending } = useTaskPause();
const { open: openItemDialog } = useAgentItemDialogs();

const PRESETS = [
	{ label: "Today", days: 0 },
	{ label: "7d", days: 7 },
	{ label: "30d", days: 30 },
];
const activePreset = ref<number | null>(7);

function applyPreset(days: number) {
	activePreset.value = days;
	jobs.setPreset(days);
}

function onDatesChanged() {
	activePreset.value = null;
	void jobs.fetch();
	void jobs.fetchTaskCompiles();
}

const tasks = computed(
	(): AgentTask[] =>
		settings.config?.agents
			.find((a) => a.name === props.agent)
			?.tasks?.filter((t) => t.name && !isWorkflow(t)) ?? [],
);

// ── Upcoming fires, as the scheduler predicts them ──────────────────────────

interface Preview {
	description: string;
	next: string[];
}
const previews = ref<Record<string, Preview | null>>({});

async function loadPreviews() {
	const agent = props.agent;
	const out: Record<string, Preview | null> = {};
	// One at a time: the live server's MCP transport prefers sequential calls.
	for (const t of tasks.value) {
		try {
			const raw = parseToolJSON(
				await callTool("task_preview", {
					name: `${agent}/${t.name}`,
					count: 20,
				}),
			) as Partial<Preview>;
			out[t.name] = {
				description: String(raw.description ?? ""),
				next: Array.isArray(raw.next) ? raw.next.map(String) : [],
			};
		} catch {
			// A watch-only task has no schedule trigger to preview.
			out[t.name] = null;
		}
	}
	if (agent === props.agent) previews.value = out;
}

// ── Rows ────────────────────────────────────────────────────────────────────

const now = ref(Date.now());

function taskID(name: string) {
	return `${props.agent}/${name}`;
}

function scheduleText(task: AgentTask, enabled: boolean): string {
	const p = previews.value[task.name];
	let text = p?.description || "";
	if (!text && task.watch) text = `when ${task.watch} changes`;
	if (!text) text = p === undefined ? "…" : "manual only";
	if (!enabled) return `${text} · paused`;
	const next = p?.next.find((n) => Date.parse(n) > now.value);
	return next ? `${text} · next ${until(next, now.value)}` : text;
}

const rowsAll = computed((): TaskRow[] =>
	tasks.value.map((task) => {
		const id = taskID(task.name);
		const runs = jobs.jobs.filter((j) => j.task_id === id);
		const enabled = task.enabled !== false;
		return {
			id,
			task,
			enabled,
			owner:
				task.type === "project" && task.project
					? `project ${task.project}`
					: `agent ${props.agent}`,
			schedule: scheduleText(task, enabled),
			running: runs.some((j) => j.status === "in_progress"),
			// Jobs are newest first.
			last: runs.find((j) => j.status !== "pending"),
		};
	}),
);

type Filter = "all" | "enabled" | "paused" | "running" | "failed";
const FILTERS: { key: Filter; label: string }[] = [
	{ key: "all", label: "All" },
	{ key: "enabled", label: "Enabled" },
	{ key: "paused", label: "Paused" },
	{ key: "running", label: "Running" },
	{ key: "failed", label: "Failed" },
];
const filter = ref<Filter>("all");
const search = ref("");

function matchesFilter(r: TaskRow, f: Filter): boolean {
	switch (f) {
		case "enabled":
			return r.enabled;
		case "paused":
			return !r.enabled;
		case "running":
			return r.running;
		case "failed":
			return r.last?.status === "failed";
		default:
			return true;
	}
}

function filterCount(f: Filter): number {
	return rowsAll.value.filter((r) => matchesFilter(r, f)).length;
}

const rows = computed(() => {
	const q = search.value.trim().toLowerCase();
	return rowsAll.value.filter(
		(r) =>
			matchesFilter(r, filter.value) &&
			(!q ||
				[r.task.name, r.owner, r.schedule, r.task.prompt ?? ""].some((s) =>
					s.toLowerCase().includes(q),
				)),
	);
});

const RUN_LABEL: Record<Job["status"], string> = {
	pending: "Pending",
	in_progress: "Running",
	completed: "Succeeded",
	failed: "Failed",
	canceled: "Canceled",
};
const RUN_CLASS: Record<Job["status"], string> = {
	pending: "text-yellow-600 dark:text-yellow-400",
	in_progress: "text-blue-600 dark:text-blue-400",
	completed: "text-green-600 dark:text-green-400",
	failed: "text-red-600 dark:text-red-400",
	canceled: "text-amber-600 dark:text-amber-400",
};

function ago(iso: string): string {
	const a = age(iso, now.value);
	return a ? `${a} ago` : "";
}

// ── Stat cards ──────────────────────────────────────────────────────────────

function median(values: number[]): number {
	const sorted = [...values].sort((a, b) => a - b);
	const mid = Math.floor(sorted.length / 2);
	return sorted.length % 2 ? sorted[mid] : (sorted[mid - 1] + sorted[mid]) / 2;
}

const statCards = computed(() => {
	const finished = [...jobs.completed, ...jobs.failed];
	const durations = finished.map(jobDurationMs).filter((ms) => ms >= 0);
	const avg = durations.length
		? durations.reduce((a, b) => a + b, 0) / durations.length
		: 0;
	const dayAhead = now.value + 24 * 3600 * 1000;
	const upcoming = rowsAll.value
		.filter((r) => r.enabled)
		.reduce(
			(n, r) =>
				n +
				(previews.value[r.task.name]?.next ?? []).filter((iso) => {
					const t = Date.parse(iso);
					return t > now.value && t <= dayAhead;
				}).length,
			0,
		);
	const lastFailure = jobs.failed[0];
	return [
		{
			label: "Success rate",
			testid: "stat-success-rate",
			value: finished.length
				? `${Math.round((jobs.completed.length / finished.length) * 100)}%`
				: "—",
			caption: `${finished.length} finished`,
			tone: "green" as const,
		},
		{
			label: "Avg duration",
			testid: "stat-duration",
			value: durations.length ? formatDuration(avg) : "—",
			caption: durations.length
				? `median ${formatDuration(median(durations))}`
				: "",
			tone: "neutral" as const,
		},
		{
			label: "Next 24h",
			testid: "stat-upcoming",
			value: upcoming,
			caption: "runs coming up",
			tone: "purple" as const,
		},
		{
			label: "Running",
			testid: "stat-running",
			value: jobs.running.length,
			caption: `${jobs.pending.length} pending`,
			pulse: jobs.running.length > 0,
			tone: "blue" as const,
		},
		{
			label: "Failures",
			testid: "stat-failures",
			value: jobs.failed.length,
			caption: lastFailure
				? `last ${lastFailure.task_id.split("/").pop()} ${ago(lastFailure.created_at)}`
				: "none in range",
			tone: "red" as const,
		},
	];
});

function sparkH(count: number): string {
	const max = Math.max(...jobs.byDay.map((r) => r.completed + r.failed), 1);
	return `${Math.max(2, Math.round((count / max) * 32))}px`;
}

// ── Layout, remembered across reloads ───────────────────────────────────────

type Layout = "cards" | "list";
const LAYOUT_KEY = "aviary.tasks.layout";
const LAYOUTS: { key: Layout; label: string; icon: unknown }[] = [
	{ key: "cards", label: "Show tasks as cards", icon: Squares2X2Icon },
	{ key: "list", label: "Show tasks as a list", icon: ListBulletIcon },
];
const layout = ref<Layout>(
	localStorage.getItem(LAYOUT_KEY) === "list" ? "list" : "cards",
);
watch(layout, (l) => localStorage.setItem(LAYOUT_KEY, l));

// ── Actions ─────────────────────────────────────────────────────────────────

const analyticsOpen = ref(false);
const runsTask = ref("");
const busy = ref("");
const actionError = ref("");

// A task opens on its page, as its sidebar row does; settings are behind
// its gear.
function openTask(name: string) {
	void router.push(taskSessionPath(props.agent, name));
}

function openSettings(name: string) {
	openItemDialog({ kind: "task", agent: props.agent, mode: "edit", name });
}

function viewRuns(r: TaskRow) {
	runsTask.value = r.id;
	analyticsOpen.value = true;
}

function onAction(r: TaskRow, action: TaskAction) {
	if (action === "run") void runNow(r);
	else if (action === "toggle") void setEnabled(r, !r.enabled);
	else if (action === "recompile") void recompile(r);
	else if (action === "settings") openSettings(r.task.name);
	else if (action === "duplicate")
		openItemDialog({
			kind: "task",
			agent: props.agent,
			mode: "create",
			duplicate: r.task.name,
		});
	else viewRuns(r);
}

async function act(r: TaskRow, fn: () => Promise<unknown>) {
	busy.value = r.id;
	actionError.value = "";
	try {
		await fn();
	} catch (e) {
		actionError.value = e instanceof Error ? e.message : String(e);
	} finally {
		busy.value = "";
	}
}

function runNow(r: TaskRow) {
	return act(r, () => jobs.runTaskNow(r.id));
}

// Recompile discards the failing script and compiles a fresh one in the
// background; precompile stays on.
function recompile(r: TaskRow) {
	return act(r, async () => {
		await callTool("config_task_convert_to_script", {
			agent: props.agent,
			task: r.task.name,
		});
		await settings.fetchConfig();
	});
}

// Optimistic: the switch, the sidebar and the editor flip at once, and a
// failure flips them back with a toast.
function setEnabled(r: TaskRow, enabled: boolean) {
	return setTaskEnabled(props.agent, r.task.name, enabled);
}

// ── Loading and refreshing ──────────────────────────────────────────────────

async function load() {
	jobs.setAgent(props.agent);
	await jobs.refreshAll();
	await loadPreviews();
}

watch(() => props.agent, load);
watch(
	() =>
		tasks.value
			.map((t) =>
				[
					t.name,
					t.enabled,
					t.schedule,
					t.every,
					t.at,
					t.days,
					t.timezone,
					t.start_at,
					t.trigger,
				].join("|"),
			)
			.join("\n"),
	() => void loadPreviews(),
);

let timer: ReturnType<typeof setInterval> | undefined;
onMounted(() => {
	void load();
	// Runs start and finish on their own, so the page keeps itself current.
	timer = setInterval(() => {
		now.value = Date.now();
		if (document.visibilityState === "visible") void jobs.fetch();
	}, 15_000);
});
onUnmounted(() => clearInterval(timer));
</script>
