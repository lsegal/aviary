<template>
	<AppLayout>
		<div class="flex h-full min-h-0">
			<div class="flex h-full min-w-0 flex-1 flex-col">
			<div class="min-h-0 flex-1 overflow-y-auto">
				<template v-if="project || aggregate">
					<header
						class="flex flex-col gap-3 border-b border-gray-200 px-4 py-4 dark:border-gray-800 sm:px-6 xl:flex-row xl:items-center">
						<div class="min-w-0">
							<p v-if="aggregate" class="text-xs text-gray-500">{{ routeAgent }} /</p>
							<p v-else class="text-xs text-gray-500"><router-link :to="projectsIndexPath" class="hover:underline">{{ projectsIndexLabel }}</router-link> /</p>
							<div class="flex flex-wrap items-center gap-x-2 gap-y-1">
								<h1 v-if="aggregate" class="truncate text-xl font-bold text-gray-900 dark:text-white">GitHub Projects</h1>
								<span v-if="aggregate" data-testid="board-projects"
									class="inline-flex shrink-0 items-center rounded-full bg-gray-100 px-2 py-0.5 text-xs font-medium text-gray-700 dark:bg-gray-800 dark:text-gray-200"
									:title="targets.join(', ')">{{ targets.length }} {{ targets.length === 1 ? "project" : "projects" }}</span>
								<h1 v-else class="truncate text-xl font-bold text-gray-900 dark:text-white">
									<a :href="targetURL(project)" target="_blank" rel="noopener noreferrer" class="hover:underline"
										:title="`Open ${project} on GitHub`">{{ project }}</a>
								</h1>
								<router-link v-if="owner" :to="settingsPath" data-testid="project-owner"
									class="inline-flex shrink-0 items-center rounded-full bg-gray-100 px-2 py-0.5 text-xs font-medium text-gray-700 hover:bg-gray-200 dark:bg-gray-800 dark:text-gray-200 dark:hover:bg-gray-700"
									:title="`${project} belongs to agent ${owner}`">agent: {{ owner }}</router-link>
								<span data-testid="delivery-status" class="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium"
									:class="online ? 'bg-green-50 text-green-700 dark:bg-green-950/50 dark:text-green-300' : 'bg-red-50 text-red-700 dark:bg-red-950/50 dark:text-red-300'">
									<span class="h-1.5 w-1.5 rounded-full" :class="online ? 'bg-green-500' : 'bg-red-500'" />
									{{ online ? "live" : "offline" }}
								</span>
								<span v-if="status?.instanceID" data-testid="instance-id"
									class="inline-flex shrink-0 items-center gap-1.5 rounded-full border border-blue-200 bg-blue-50 px-2.5 py-0.5 text-xs font-medium text-blue-800 dark:border-blue-900 dark:bg-blue-950/50 dark:text-blue-200"
									:title="`This instance signs its GitHub claims as /aviary:${status.instanceID}`">
									Instance <code class="font-mono text-sm font-bold tracking-wider">{{ status.instanceID }}</code>
								</span>
							</div>
							<p v-if="status" class="text-xs text-gray-500 dark:text-gray-400">
								Issues labeled <b>{{ status.readyLabel }}</b>
								<template v-if="status.delivery"> · {{ status.delivery }}</template>
								<template v-if="status.lastChecked"> · checked {{ age(status.lastChecked, now) }} ago</template>
								<template v-if="status.interval"> · every {{ status.interval }}</template>
							</p>
						</div>
						<div class="flex flex-wrap items-center gap-2 xl:ml-auto">
							<button v-if="view === 'board'" type="button" aria-label="Check now" title="Check now"
								class="rounded-lg border border-gray-200 p-1.5 text-gray-700 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800"
								:disabled="checking" @click="onCheckNow">
								<ArrowPathIcon aria-hidden="true" class="h-5 w-5" :class="{ 'animate-spin': checking }" />
							</button>
							<button v-if="view === 'board'" type="button" data-testid="logs-toggle" :aria-pressed="logsOpen"
								aria-label="Logs" title="Logs"
								class="rounded-lg border p-1.5"
								:class="logsOpen ? 'border-blue-600 bg-blue-600 text-white' : 'border-gray-200 text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800'"
								@click="logsOpen = !logsOpen">
								<DocumentTextIcon aria-hidden="true" class="h-5 w-5" />
							</button>
							<button v-if="view === 'board'" type="button" data-testid="prompt-chat-toggle" :aria-pressed="chatOpen"
								aria-label="Prompt chat" title="Prompt chat"
								class="rounded-lg border p-1.5"
								:class="chatOpen ? 'border-blue-600 bg-blue-600 text-white' : 'border-gray-200 text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800'"
								@click="chatOpen = !chatOpen">
								<ChatBubbleLeftRightIcon aria-hidden="true" class="h-5 w-5" />
							</button>
							<router-link :to="settingsPath" data-testid="project-settings-link" aria-label="Settings" title="Settings"
								class="rounded-lg border border-gray-200 p-1.5 text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800">
								<Cog6ToothIcon aria-hidden="true" class="h-5 w-5" />
							</router-link>
						</div>
					</header>

					<div class="space-y-4 px-4 py-4 sm:px-6">
						<GitHubSignInBanner v-if="status?.github" :session="status.github" :busy="signingIn"
							@signin="onGitHub(signInToGitHub)" @use-polling="onGitHub(switchToPolling)"
							@import="onGitHub(() => importProfile($event))" />
						<p v-if="status && !status.running" class="rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-800 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-300">
							The watch isn't running{{ status.error ? `: ${status.error}` : "." }}
						</p>
						<p v-else-if="status?.error" class="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900 dark:bg-red-950/40 dark:text-red-300">
							{{ status.error }}
						</p>
						<p v-if="error" class="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900 dark:bg-red-950/40 dark:text-red-300">
							{{ error }}
						</p>

						<template v-if="view === 'board'">
						<UpNextStrip v-if="!aggregate" :runs="upcoming" :now="now" @open="openSchedule" />

						<section v-if="status" class="grid grid-cols-2 gap-3 sm:grid-cols-4 xl:grid-cols-7" aria-label="Job counts">
							<div v-for="s in statCards" :key="s.label" class="rounded-xl border px-4 py-3" :class="s.cls">
								<p class="text-xs font-semibold uppercase tracking-wide opacity-80">{{ s.label }}</p>
								<p class="text-2xl font-bold">{{ s.value }}<span v-if="s.suffix" class="ml-1 text-sm font-normal opacity-70">{{ s.suffix }}</span></p>
							</div>
						</section>

						<section v-if="status?.clis.length" class="flex flex-wrap items-center gap-2 text-xs" aria-label="CLI providers">
							<span class="font-medium text-gray-600 dark:text-gray-300">CLI providers</span>
							<span v-for="cli in status.clis" :key="cli.name"
								class="inline-flex items-center gap-1.5 rounded-full border border-gray-200 px-2.5 py-1 dark:border-gray-700">
								<span class="h-1.5 w-1.5 rounded-full" :class="cli.signedIn ? 'bg-green-500' : 'bg-gray-400'" />
								<b class="text-gray-800 dark:text-gray-100">{{ cli.name }}</b>
								<span v-if="cli.models.length" class="text-gray-600 dark:text-gray-300">{{ cli.models.join(", ") }}</span>
								<span class="text-gray-500">{{ cli.quota || "no quota reported" }}</span>
							</span>
						</section>

						</template>

						<div class="flex flex-wrap items-center gap-3 text-sm" data-testid="job-toolbar">
							<div v-if="view === 'board'" class="relative min-w-0 flex-1 sm:max-w-xs">
								<span class="pointer-events-none absolute inset-y-0 left-3 flex items-center text-gray-400 dark:text-gray-500"
									aria-hidden="true" data-testid="search-icon">
									<MagnifyingGlassIcon class="h-4 w-4" />
								</span>
								<input v-model="search" type="search" aria-label="Search jobs" placeholder="Search jobs, logs, #issue, #PR, session…"
									class="field-input pl-9!" />
							</div>
							<div v-if="!aggregate" role="tablist" aria-label="Project views" class="flex max-w-full shrink-0 overflow-x-auto rounded-lg border border-gray-200 dark:border-gray-700">
								<button v-for="v in VIEWS" :key="v.key" type="button" role="tab" :aria-selected="view === v.key"
									class="shrink-0 px-3 py-1.5" :class="view === v.key ? 'bg-gray-900 text-white dark:bg-white dark:text-gray-900' : 'text-gray-600 hover:bg-gray-50 dark:text-gray-300 dark:hover:bg-gray-800'"
									@click="setView(v.key)">
									{{ v.key === "schedules" && schedules.length ? `${v.label} ${schedules.length}` : v.label }}
								</button>
							</div>
							<template v-if="view === 'board'">
								<div role="tablist" aria-label="Job filter" class="flex max-w-full overflow-x-auto rounded-lg border border-gray-200 dark:border-gray-700">
									<button v-for="f in FILTERS" :key="f.key" type="button" role="tab" :aria-selected="filter === f.key"
										class="shrink-0 px-3 py-1.5" :class="filter === f.key ? FILTER_ACTIVE[f.key] : 'text-gray-600 hover:bg-gray-50 dark:text-gray-300 dark:hover:bg-gray-800'"
										@click="filter = f.key">
										{{ f.key === "all" && total ? `${f.label} ${total}` : f.label }}
									</button>
								</div>
								<button type="button" data-testid="kanban-toggle" :aria-pressed="kanban"
									:aria-label="kanban ? 'Show jobs as a list' : 'Show jobs as a kanban board'"
									:title="kanban ? 'Show jobs as a list' : 'Show jobs as a kanban board'"
									class="rounded-lg border p-1.5"
									:class="kanban ? 'border-blue-600 bg-blue-600 text-white' : 'border-gray-200 text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800'"
									@click="kanban = !kanban">
									<ViewColumnsIcon aria-hidden="true" class="h-5 w-5" />
								</button>
								<span v-if="searching" class="text-gray-500 dark:text-gray-400" data-testid="search-count">
									{{ total }} of {{ unfilteredTotal }} {{ unfilteredTotal === 1 ? "job matches" : "jobs match" }}
								</span>
								<nav v-if="!kanban && (searching || pageCount > 1)" class="flex items-center gap-3 sm:ml-auto" aria-label="Job pages">
									<button type="button" aria-label="Previous page" title="Previous page" class="rounded-lg border border-gray-200 p-1.5 text-gray-700 hover:bg-gray-50 disabled:opacity-40 disabled:hover:bg-transparent dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800"
										:disabled="page === 0" @click="page--">
										<ChevronLeftIcon aria-hidden="true" class="h-4 w-4" />
									</button>
									<span class="text-gray-500">Page {{ page + 1 }} of {{ Math.max(pageCount, 1) }}</span>
									<button type="button" aria-label="Next page" title="Next page" class="rounded-lg border border-gray-200 p-1.5 text-gray-700 hover:bg-gray-50 disabled:opacity-40 disabled:hover:bg-transparent dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800"
										:disabled="page + 1 >= pageCount" @click="page++">
										<ChevronRightIcon aria-hidden="true" class="h-4 w-4" />
									</button>
								</nav>
							</template>
						</div>

						<SchedulesPanel v-if="view === 'schedules'" v-model:selected="selectedSchedule" :project="project"
							:schedules="schedules" :agents="owner ? [owner] : scheduleAgents" :error="scheduleError" :loaded="schedulesLoaded" :now="now"
							:save="saveSchedule" :set-enabled="setScheduleEnabled" :remove="removeSchedule" :run-now="runScheduleNow"
							:preview="previewSchedule" :refresh="refreshSchedules" />

						<ScheduleDetail v-else-if="detailSchedule" :schedule="detailSchedule" :now="now" :back="{ query: {} }"
							:edit="{ query: { tab: 'schedules', schedule: detailSchedule.id } }" :predict="predictSchedule"
							:run-now="runScheduleNow" />

						<template v-else>
						<div v-if="loading && !jobs.length && !boardSchedules.length" class="py-12 text-center text-sm text-gray-400">Loading jobs…</div>
						<KanbanBoard v-else-if="kanban" :jobs="jobs" :filter="filter" :now="now" :schedules="boardSchedules"
							:schedule-to="aggregate ? undefined : scheduleTo" />
						<div v-else-if="!jobs.length && !boardSchedules.length && searching" class="py-12 text-center text-sm text-gray-500 dark:text-gray-400" data-testid="search-empty">
							<p>No jobs match “<b class="text-gray-800 dark:text-gray-100">{{ search.trim() }}</b>”.</p>
							<button type="button" class="mt-3 rounded-lg border border-gray-200 px-3 py-1.5 text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800"
								@click="search = ''">Clear search</button>
						</div>
						<div v-else-if="aggregate && !targets.length" class="py-12 text-center text-sm text-gray-400" data-testid="board-no-projects">
							{{ routeAgent }} owns no projects yet.
							<button type="button" class="text-blue-600 hover:underline dark:text-blue-400" @click="openAdd">Add one</button>.
						</div>
						<div v-else-if="!jobs.length && !boardSchedules.length" class="py-12 text-center text-sm text-gray-400">No jobs here yet.</div>
						<section v-else class="grid gap-4 md:grid-cols-2 2xl:grid-cols-3" aria-label="Jobs">
							<ScheduledCard v-for="s in boardSchedules" :key="s.id" :schedule="s" :now="now" :to="scheduleTo(s)" />
							<JobCard v-for="job in jobs" :key="job.id" :job="job" :now="now" :show-project="aggregate"
								:busy="busyJob === job.id" @retry="onAction(retry, $event)" @stop="onAction(stop, $event)"
								@summarize="onSummarize" />
						</section>
						</template>

						<footer v-if="status" class="flex flex-wrap gap-x-4 gap-y-1 text-xs text-gray-500 dark:text-gray-400">
							<span v-if="status.targets.length">
								Targets:
								<template v-for="(t, i) in status.targets" :key="t.name">{{ i ? ", " : "" }}{{ t.name }} ({{ t.issues }})</template>
							</span>
							<span v-if="version">Version {{ shortVersion(version) }}</span>
						</footer>
					</div>
				</template>
			</div>
			<ProjectLogsPanel v-if="(project || aggregate) && logsOpen" :lines="status?.log ?? []" @close="logsOpen = false" />
			</div>
			<ProjectChatPanel v-if="(project || aggregate) && chatOpen" ref="chatPanel" :project="project" @close="chatOpen = false" />
		</div>
		<AddProjectDialog v-if="adding" :agent="routeAgent" @close="closeAdd" @added="onAdded" />
	</AppLayout>
</template>

<script setup lang="ts">
import ArrowPathIcon from "@heroicons/vue/24/outline/ArrowPathIcon";
import ChatBubbleLeftRightIcon from "@heroicons/vue/24/outline/ChatBubbleLeftRightIcon";
import ChevronLeftIcon from "@heroicons/vue/24/outline/ChevronLeftIcon";
import ChevronRightIcon from "@heroicons/vue/24/outline/ChevronRightIcon";
import Cog6ToothIcon from "@heroicons/vue/24/outline/Cog6ToothIcon";
import DocumentTextIcon from "@heroicons/vue/24/outline/DocumentTextIcon";
import MagnifyingGlassIcon from "@heroicons/vue/24/outline/MagnifyingGlassIcon";
import ViewColumnsIcon from "@heroicons/vue/24/outline/ViewColumnsIcon";
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import AppLayout from "../components/AppLayout.vue";
import AddProjectDialog from "../components/projects/AddProjectDialog.vue";
import GitHubSignInBanner from "../components/projects/GitHubSignInBanner.vue";
import JobCard from "../components/projects/JobCard.vue";
import KanbanBoard from "../components/projects/KanbanBoard.vue";
import ProjectChatPanel from "../components/projects/ProjectChatPanel.vue";
import ProjectLogsPanel from "../components/projects/ProjectLogsPanel.vue";
import ScheduleDetail from "../components/projects/ScheduleDetail.vue";
import ScheduledCard from "../components/projects/ScheduledCard.vue";
import SchedulesPanel from "../components/projects/SchedulesPanel.vue";
import UpNextStrip from "../components/projects/UpNextStrip.vue";
import {
	JOBS_PAGE_SIZE,
	useProjectBoard,
	useProjectList,
	useProjectSchedules,
} from "../composables/useProjects";
import { useServerStatus } from "../composables/useServerStatus";
import {
	agentProjectsSettingsPath,
	projectOwner,
	projectSettingsPath,
	projectTargets,
} from "../lib/projectSettings";
import {
	age,
	BOARD_STATES,
	type BoardFilter,
	type BoardState,
	type ProjectJob,
	type ProjectSchedule,
	projectPath,
	scheduleMatches,
	stateLabel,
	summarizePrompt,
	targetURL,
} from "../lib/projects";
import { shortVersion } from "../lib/version";
import { useSettingsStore } from "../stores/settings";

const FILTERS: { key: BoardFilter; label: string }[] = [
	{ key: "all", label: "All" },
	...BOARD_STATES.map((key) => ({ key, label: stateLabel(key) })),
];
// A selected state tab takes that state's color; Starting is orange.
const FILTER_ACTIVE: Record<BoardFilter, string> = {
	all: "bg-gray-900 text-white dark:bg-white dark:text-gray-900",
	starting: "bg-orange-500 text-white",
	paused: "bg-amber-500 text-white",
	running: "bg-blue-600 text-white",
	done: "bg-green-600 text-white",
	failed: "bg-red-600 text-white",
};

type ProjectView = "board" | "schedules";
const VIEWS: { key: ProjectView; label: string }[] = [
	{ key: "board", label: "Board" },
	{ key: "schedules", label: "Schedules" },
];

const route = useRoute();
const router = useRouter();
const projectList = useProjectList();
const project = computed(() =>
	typeof route.params.project === "string" ? route.params.project : "",
);
// A project belongs to the agent whose projects list it; its settings live
// on that agent's settings page.
const settingsStore = useSettingsStore();
if (!settingsStore.config) void settingsStore.fetchConfig();
const owner = computed(() => projectOwner(settingsStore.config, project.value));
// The agent whose GitHub Projects this board sits under; a watch no agent
// owns sits under System instead.
const routeAgent = computed(() =>
	typeof route.params.agent === "string" ? route.params.agent : "",
);
const projectsIndexPath = computed(() =>
	routeAgent.value
		? `/agents/${encodeURIComponent(routeAgent.value)}/projects`
		: route.path,
);
const projectsIndexLabel = computed(() =>
	routeAgent.value ? `${routeAgent.value} · GitHub Projects` : "Watches",
);
// Without a project the route is the agent's GitHub Projects: one board of
// every project it owns.
const aggregate = computed(() => !project.value && !!routeAgent.value);
const targets = computed(() =>
	project.value
		? [project.value]
		: projectTargets(
				settingsStore.config?.agents.find((a) => a.name === routeAgent.value)
					?.projects,
			),
);
const settingsPath = computed(() =>
	aggregate.value
		? agentProjectsSettingsPath(routeAgent.value)
		: projectSettingsPath(settingsStore.config, project.value),
);
const filter = ref<BoardFilter>("all");
// The search narrows the jobs within the filter, across every field and the
// whole list rather than one page.
const search = ref("");
const searching = computed(() => search.value.trim() !== "");
// The kanban layout survives reloads, like the prompt chat's open state.
const KANBAN_KEY = "aviary.projects.kanban";
const kanban = ref(localStorage.getItem(KANBAN_KEY) === "1");
watch(kanban, (on) => localStorage.setItem(KANBAN_KEY, on ? "1" : "0"));
const { version } = useServerStatus();
const {
	status,
	jobs,
	total,
	unfilteredTotal,
	page,
	error,
	loading,
	checkNow,
	signInToGitHub,
	switchToPolling,
	importProfile,
	retry,
	stop,
} = useProjectBoard(targets, filter, search, kanban);
const {
	schedules,
	upcoming,
	agents: scheduleAgents,
	error: scheduleError,
	loaded: schedulesLoaded,
	save: saveSchedule,
	setEnabled: setScheduleEnabled,
	remove: removeSchedule,
	runNow: runScheduleNow,
	preview: previewSchedule,
	predict: predictSchedule,
	refresh: refreshSchedules,
} = useProjectSchedules(project);

// The project's scheduled tasks are their own kind of box on the board: in
// the list, under All, and in the kanban's Scheduled column. A state filter
// shows jobs only.
const boardSchedules = computed(() =>
	filter.value === "all"
		? schedules.value.filter((s) => scheduleMatches(s, search.value))
		: [],
);
// A scheduled box opens its detail view on the board, at ?scheduled=<id>.
function scheduleTo(s: ProjectSchedule) {
	return { query: { scheduled: s.id } };
}
const detailSchedule = computed(() => {
	const id = route.query.scheduled;
	return view.value === "board" && typeof id === "string"
		? (schedules.value.find((s) => s.id === id) ?? null)
		: null;
});

// The view and the schedule being edited live in the URL, so a reload or a
// shared link opens the same place.
const view = computed<ProjectView>(() =>
	route.query.tab === "schedules" ? "schedules" : "board",
);
const selectedSchedule = computed({
	get: () =>
		typeof route.query.schedule === "string" ? route.query.schedule : "",
	set: (id: string) => {
		void router.replace({
			query: { ...route.query, tab: "schedules", schedule: id || undefined },
		});
	},
});
function setView(next: ProjectView) {
	void router.replace({
		query: next === "board" ? {} : { tab: next },
	});
}
function openSchedule(id: string) {
	selectedSchedule.value = id;
}

// The add-project dialog is open while the URL carries ?add=1, so the
// sidebar's "Add project" link can open it from anywhere. The new project
// belongs to the board's agent, so a watch no agent owns has no dialog.
const adding = computed(() => route.query.add === "1" && !!routeAgent.value);
function openAdd() {
	void router.push({ query: { ...route.query, add: "1" } });
}
function closeAdd() {
	const { add: _, ...rest } = route.query;
	void router.replace({ query: rest });
}
async function onAdded(target: string, agent: string) {
	await router.push(projectPath(agent, target));
	await projectList.refresh();
}

const now = ref(Date.now());
let clock: ReturnType<typeof setInterval> | undefined;
onMounted(() => {
	clock = setInterval(() => {
		now.value = Date.now();
	}, 1000);
});
onUnmounted(() => clearInterval(clock));

// A signed-out browser session reads nothing, so it counts as offline.
const online = computed(
	() =>
		(status.value?.deliveryOnline ?? true) && !status.value?.github?.signedOut,
);
const pageCount = computed(() => Math.ceil(total.value / JOBS_PAGE_SIZE));

// Each board state's stat card, in its pill's colors; Starting is orange.
const STATE_CARD: Record<BoardState, string> = {
	starting:
		"border-orange-200 bg-orange-50 text-orange-700 dark:border-orange-900 dark:bg-orange-950/40 dark:text-orange-300",
	paused:
		"border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-300",
	running:
		"border-blue-200 bg-blue-50 text-blue-700 dark:border-blue-900 dark:bg-blue-950/40 dark:text-blue-300",
	done: "border-green-200 bg-green-50 text-green-700 dark:border-green-900 dark:bg-green-950/40 dark:text-green-300",
	failed:
		"border-red-200 bg-red-50 text-red-700 dark:border-red-900 dark:bg-red-950/40 dark:text-red-300",
};

const statCards = computed<
	{ label: string; value: number; suffix?: string; cls: string }[]
>(() => {
	const s = status.value;
	if (!s) return [];
	return [
		{
			label: "Slots",
			value: s.slotsUsed,
			suffix: `of ${s.slotsTotal} busy`,
			cls: "border-gray-200 text-gray-900 dark:border-gray-800 dark:text-white",
		},
		...BOARD_STATES.map((state) => ({
			label: stateLabel(state),
			value: s.states[state],
			cls: STATE_CARD[state],
		})),
		{
			label: "Merged (24h)",
			value: s.merged24h,
			cls: "border-purple-200 bg-purple-50 text-purple-700 dark:border-purple-900 dark:bg-purple-950/40 dark:text-purple-300",
		},
	];
});

const busyJob = ref("");
async function onAction(
	fn: (job: ProjectJob) => Promise<void>,
	job: ProjectJob,
) {
	busyJob.value = job.id;
	try {
		await fn(job);
	} catch (e) {
		error.value = e instanceof Error ? e.message : String(e);
	} finally {
		busyJob.value = "";
	}
}

const checking = ref(false);
async function onCheckNow() {
	checking.value = true;
	try {
		await checkNow();
	} catch (e) {
		error.value = e instanceof Error ? e.message : String(e);
	} finally {
		checking.value = false;
	}
}

// The signed-out banner's actions: sign in to GitHub in Aviary's browser,
// switch to gh polling, or import a profile signed in elsewhere.
const signingIn = ref(false);
async function onGitHub(fn: () => Promise<void>) {
	signingIn.value = true;
	try {
		await fn();
		error.value = "";
	} catch (e) {
		error.value = e instanceof Error ? e.message : String(e);
	} finally {
		signingIn.value = false;
	}
}

// The prompt chat's open state survives reloads.
const CHAT_OPEN_KEY = "aviary.projects.chatOpen";
const chatOpen = ref(localStorage.getItem(CHAT_OPEN_KEY) === "1");
watch(chatOpen, (open) =>
	localStorage.setItem(CHAT_OPEN_KEY, open ? "1" : "0"),
);
// The logs panel's open state survives reloads too.
const LOGS_OPEN_KEY = "aviary.projects.logsOpen";
const logsOpen = ref(localStorage.getItem(LOGS_OPEN_KEY) === "1");
watch(logsOpen, (open) =>
	localStorage.setItem(LOGS_OPEN_KEY, open ? "1" : "0"),
);
const chatPanel = ref<{ send: (text: string) => Promise<void> } | null>(null);

async function onSummarize(job: ProjectJob) {
	chatOpen.value = true;
	await nextTick();
	await chatPanel.value?.send(summarizePrompt(job));
}
</script>
