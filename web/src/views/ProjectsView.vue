<template>
	<AppLayout :compact-header="!jobOpen"
		:title-link="project && !jobOpen ? { href: targetURL(project), title: `Open ${project} on GitHub` } : null">
		<template v-if="(project || aggregate) && !jobOpen" #header-status>
				<span v-if="aggregate" data-testid="board-projects"
					class="inline-flex shrink-0 items-center rounded-full bg-gray-100 px-2 py-0.5 text-xs font-medium text-gray-700 dark:bg-gray-800 dark:text-gray-200"
					:title="targets.join(', ')">{{ targets.length }} {{ targets.length === 1 ? "project" : "projects" }}</span>
				<span data-testid="delivery-status" class="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium"
					:class="online ? 'bg-green-50 text-green-700 dark:bg-green-950/50 dark:text-green-300' : 'bg-red-50 text-red-700 dark:bg-red-950/50 dark:text-red-300'">
					<span class="h-1.5 w-1.5 rounded-full" :class="online ? 'bg-green-500' : 'bg-red-500'" />
					{{ online ? "live" : "offline" }}
				</span>
				<span v-if="manual" data-testid="project-manual-chip"
					class="inline-flex shrink-0 items-center rounded-full bg-amber-50 px-2 py-0.5 text-xs font-medium text-amber-800 dark:bg-amber-950/50 dark:text-amber-200"
					title="Manual mode: this project is never searched for issues; only work you queue, scheduled tasks and agent tools start jobs">Manual</span>
				<span v-if="issueSummary" data-testid="board-issues"
					class="inline-flex shrink-0 items-center rounded-full bg-gray-100 px-2 py-0.5 text-xs font-medium text-gray-700 dark:bg-gray-800 dark:text-gray-200"
					:title="issueSummary.title">{{ issueSummary.label }}</span>
				<BoardHeaderInfo v-if="status" :status="status" :manual="manual" :filter-target="filterTarget" :now="now"
					:hide-instance="inMenu.has('instance-tag')" :hide-info="inMenu.has('board-info')" @saved="refresh" />
		</template>
		<template v-if="project || aggregate" #header-actions>
			<!-- With a job open on a phone, the job's own actions keep the row
				and the board's move behind "…" (#1347). -->
			<HeaderOverflow :collapsed="jobOpen && !smUp" label="Board actions">
			<!-- On a phone the header is one line, so everything but "…" moves
				into "…" one at a time until it fits: Logs, Watch, Chat,
				Schedule, Queue and Add project, then the details and instance
				icons (#1576). -->
			<button v-if="aggregate && !inMenu.has('board-add-project')" type="button" data-testid="board-add-project" aria-label="Add project" title="Add project"
				:class="HEADER_ADD_CLS" @click="openAdd">
				<PlusIcon aria-hidden="true" class="h-5 w-5" />
			</button>
			<QueueMenu v-if="canQueue && !inMenu.has('queue-menu')" :no-merge="status?.noMerge"
				@pick="openQueue($event, project || targets[0])" />
			<button v-if="!aggregate && !inMenu.has('schedule-new')" type="button" data-testid="schedule-new" aria-label="Schedule task"
				title="Schedule task" :class="HEADER_COLLAPSE_CLS" :disabled="!scheduleAgent" @click="onNewSchedule">
				<CalendarDaysIcon aria-hidden="true" :class="HEADER_COLLAPSE_ICON_CLS" />
				<span :class="HEADER_COLLAPSE_LABEL_CLS">Schedule</span>
			</button>
			<!-- Logs, then Watch, move into "…" when the row is too narrow;
				Refresh, Clear and Settings always live there (#1488). -->
			<button v-if="!logsInMenu" type="button" data-testid="logs-toggle" :aria-pressed="logsOpen"
				aria-label="Logs" title="Logs"
				:class="logsOpen ? HEADER_ICON_ON_CLS : HEADER_ICON_CLS"
				@click="logsOpen = !logsOpen">
				<DocumentTextIcon aria-hidden="true" class="h-5 w-5" />
			</button>
			<button v-if="!inMenu.has('prompt-chat-toggle')" type="button" data-testid="prompt-chat-toggle" :aria-pressed="chatOpen"
				aria-label="Prompt chat" title="Prompt chat"
				:class="chatOpen ? HEADER_ICON_ON_CLS : HEADER_ICON_CLS"
				@click="chatOpen = !chatOpen">
				<ChatBubbleLeftRightIcon aria-hidden="true" class="h-5 w-5" />
			</button>
			<ProjectModeMenu v-if="filterTarget && !watchInMenu" :target="filterTarget" :manual="manual"
				:default-mode="status?.defaultMode ?? 'watch'" @changed="refresh" />
			<BoardMoreMenu ref="moreMenu" :targets="targets" :done="status?.states?.done ?? 0"
				:failed="status?.states?.failed ?? 0" :checking="checking" :settings-path="settingsPath"
				:logs-in-menu="logsInMenu" v-model:logs-open="logsOpen"
				:mode-target="watchInMenu ? filterTarget : null" :manual="manual"
				:default-mode="status?.defaultMode ?? 'watch'"
				:hidden="[...inMenu]" :status="status" :filter-target="filterTarget" :now="now"
				v-model:chat-open="chatOpen" :can-schedule="!!scheduleAgent"
				@refresh="onCheckNow" @cleared="refresh" @mode-changed="refresh" @saved="refresh"
				@add-project="openAdd" @queue="openQueue($event, project || targets[0])" @schedule="onNewSchedule" />
			</HeaderOverflow>
		</template>
		<div class="flex min-h-0 flex-1">
			<div class="flex h-full min-w-0 flex-1 flex-col">
			<div class="flex min-h-0 flex-1 flex-col overflow-y-auto">
				<template v-if="project || aggregate">
					<router-view v-if="jobOpen" />
					<div v-else class="space-y-4 px-4 py-4 sm:px-6">
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
						<p v-if="scheduleError" class="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900 dark:bg-red-950/40 dark:text-red-300">
							{{ scheduleError }}
						</p>

						<UpNextStrip :runs="upcoming" :now="now" :show-project="aggregate" @open="openSchedule" />

						<!-- The counts show as full cards or as one compact row of chips, the
							default below md, and a toggle switches between them. -->
						<div v-if="status" class="flex min-w-0 gap-2" :class="statsCompact ? 'items-center' : 'items-start'">
							<!-- Columns follow the width the board actually has, not the viewport, so
								the cards reflow onto more rows instead of squeezing their values. -->
							<section v-if="!statsCompact" id="job-counts" class="grid min-w-0 flex-1 grid-cols-[repeat(auto-fill,minmax(8rem,1fr))] gap-2" aria-label="Job counts">
								<div v-for="s in statCards" :key="s.label" class="min-w-0 rounded-lg border px-3 py-1.5" :class="s.cls" data-testid="stat-card">
									<p class="truncate text-xs font-semibold uppercase tracking-wide opacity-80" :title="s.label" data-testid="stat-label">{{ s.label }}</p>
									<p class="truncate whitespace-nowrap text-xl font-bold" data-testid="stat-value">{{ s.value }}<span v-if="s.suffix" class="ml-1 text-sm font-normal opacity-70">{{ s.suffix }}</span></p>
								</div>
							</section>
							<StatChipRow v-else id="job-counts" aria-label="Job counts" :cards="statCards" />
							<button type="button" data-testid="stats-toggle" aria-controls="job-counts" :aria-expanded="!statsCompact"
								:aria-label="statsCompact ? 'Show job counts as cards' : 'Show job counts as a compact row'"
								:title="statsCompact ? 'Show job counts as cards' : 'Show job counts as a compact row'"
								class="shrink-0 rounded-lg border border-gray-200 p-1 text-gray-600 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800"
								@click="statsChoice = !statsCompact">
								<ChevronDownIcon v-if="statsCompact" aria-hidden="true" class="h-4 w-4" />
								<ChevronUpIcon v-else aria-hidden="true" class="h-4 w-4" />
							</button>
						</div>

						<!-- The projects' pills lead into each project, left of the CLI providers. -->
						<div v-if="(aggregate && targets.length) || status?.clis.length" class="flex min-w-0 flex-wrap items-center gap-2" data-testid="board-nav-row">
						<ProjectNavRow v-if="aggregate && targets.length" :agent="routeAgent" :targets="targets" />
						<section v-if="status?.clis.length" class="ml-auto flex max-w-full flex-wrap items-center gap-2 text-xs" aria-label="CLI providers">
							<span class="font-medium text-gray-600 dark:text-gray-300">CLI providers</span>
							<span v-for="cli in status.clis" :key="cli.name"
								class="inline-flex max-w-full flex-wrap items-center gap-x-1.5 rounded-full border border-gray-200 px-2.5 py-1 dark:border-gray-700">
								<span class="h-1.5 w-1.5 rounded-full" :class="cli.signedIn ? 'bg-green-500' : 'bg-gray-400'" />
								<b class="text-gray-800 dark:text-gray-100">{{ cli.name }}</b>
								<span v-if="cli.models.length" class="text-gray-600 dark:text-gray-300">{{ cli.models.join(", ") }}</span>
								<span class="text-gray-500">{{ cli.quota || "no quota reported" }}</span>
							</span>
						</section>
						</div>

						<!-- Search keeps a usable width; the state filters fold into "Filters"
							when the toolbar can't fit them beside it (#1490). -->
						<div ref="toolbar" class="flex flex-wrap items-center gap-x-3 gap-y-2 text-sm" data-testid="job-toolbar">
							<div class="relative min-w-0" :class="toolbarStacked ? 'basis-full' : 'max-w-80 min-w-[220px] flex-[1_1_220px]'">
								<span class="pointer-events-none absolute inset-y-0 left-3 flex items-center text-gray-400 dark:text-gray-500"
									aria-hidden="true" data-testid="search-icon">
									<MagnifyingGlassIcon class="h-4 w-4" />
								</span>
								<input v-model="search" type="search" aria-label="Search jobs" placeholder="Search jobs, logs, #issue, #PR, session…"
									class="field-input h-9 py-0! pl-9!" />
							</div>
							<BoardStateFilter ref="stateFilter" :filters="filters" :counts="filterCounts" :collapsed="filtersCollapsed"
								@toggle="toggleState" @clear="setStates([])" />
							<button ref="kanbanButton" type="button" data-testid="kanban-toggle" :aria-pressed="kanban"
								:aria-label="kanban ? 'Show jobs as a list' : 'Show jobs as a kanban board'"
								:title="kanban ? 'Show jobs as a list' : 'Show jobs as a kanban board'"
								class="inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border"
								:class="kanban ? 'border-blue-600 bg-blue-600 text-white' : 'border-gray-200 text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800'"
								@click="kanban = !kanban">
								<ViewColumnsIcon aria-hidden="true" class="h-5 w-5" />
							</button>
								<span v-if="searching" ref="toolbarCount" class="shrink-0 whitespace-nowrap text-gray-500 dark:text-gray-400" data-testid="search-count">
									{{ shownTotal }} of {{ unfilteredShown }} {{ unfilteredShown === 1 ? `${noun} matches` : `${noun}s match` }}
								</span>
								<span v-else ref="toolbarCount" class="shrink-0 whitespace-nowrap text-gray-500 dark:text-gray-400" data-testid="job-total">
									<template v-if="filters.length">{{ shownTotal }} of {{ allItems }} {{ allItems === 1 ? noun : `${noun}s` }}</template>
									<template v-else>{{ shownTotal }} {{ shownTotal === 1 ? noun : `${noun}s` }}</template>
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
						</div>

						<div v-if="loading && !boardJobs.length && !boardSchedules.length" class="py-12 text-center text-sm text-gray-400">Loading jobs…</div>
						<KanbanBoard v-else-if="kanban" :jobs="boardJobs" :filters="filters" :now="now" :schedules="boardListSchedules"
							:schedule-entries="scheduleEntries" :show-project="aggregate" @settings="openScheduleSettings" />
						<div v-else-if="!boardJobs.length && !boardSchedules.length && searching" class="py-12 text-center text-sm text-gray-500 dark:text-gray-400" data-testid="search-empty">
							<p>No jobs match “<b class="text-gray-800 dark:text-gray-100">{{ search.trim() }}</b>”.</p>
							<button type="button" class="mt-3 rounded-lg border border-gray-200 px-3 py-1.5 text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800"
								@click="search = ''">Clear search</button>
						</div>
						<div v-else-if="aggregate && !targets.length" class="py-12 text-center text-sm text-gray-400" data-testid="board-no-projects">
							{{ routeAgent }} owns no projects yet.
							<button type="button" class="text-blue-600 hover:underline dark:text-blue-400" @click="openAdd">Add one</button>.
						</div>
						<div v-else-if="!boardJobs.length && !boardSchedules.length" class="py-12 text-center text-sm text-gray-400">No jobs here yet.</div>
						<section v-else class="grid gap-4 md:grid-cols-2 2xl:grid-cols-3" aria-label="Jobs">
							<template v-for="item in boardList" :key="item.kind === 'job' ? item.job.id : item.schedule.id">
								<ScheduledCard v-if="item.kind === 'schedule'" :schedule="item.schedule" :now="now"
									:show-project="aggregate"
									:job="scheduleJob(scheduleEntries, item.schedule.id)" :busy="busySchedule === item.schedule.id"
									@run="onScheduleAction(runScheduleNow, $event)"
									@toggle="onScheduleAction(toggleSchedule, $event)" @settings="openScheduleSettings"
									@duplicate="duplicateSchedule" @dismiss="onAction(dismiss, $event)" />
								<JobCard v-else :job="item.job" :now="now" :show-project="aggregate"
									:busy="busyJob === item.job.id" @retry="onAction(retry, $event)" @stop="onAction(stop, $event)"
									@clear="onAction(clear, $event)" @summarize="onSummarize" />
							</template>
						</section>

					</div>
				</template>
			</div>
			<ProjectLogsPanel v-if="(project || aggregate) && logsOpen" :lines="status?.log ?? []" @close="logsOpen = false" />
			</div>
			<ProjectChatPanel v-if="(project || aggregate) && chatOpen" ref="chatPanel" :project="project" @close="chatOpen = false"
				@turn-end="void refreshSchedules()" />
		</div>
		<QueueDialog :target="queueTarget" :skill="queueSkill" :no-merge="status?.noMerge"
			@close="queueSkill = null" @queued="refresh" />
		<AddProjectDialog v-if="adding" :agent="routeAgent" @close="closeAdd" @added="onAdded" />
	</AppLayout>
</template>

<script setup lang="ts">
import CalendarDaysIcon from "@heroicons/vue/24/outline/CalendarDaysIcon";
import ChatBubbleLeftRightIcon from "@heroicons/vue/24/outline/ChatBubbleLeftRightIcon";
import ChevronDownIcon from "@heroicons/vue/24/outline/ChevronDownIcon";
import ChevronLeftIcon from "@heroicons/vue/24/outline/ChevronLeftIcon";
import ChevronRightIcon from "@heroicons/vue/24/outline/ChevronRightIcon";
import ChevronUpIcon from "@heroicons/vue/24/outline/ChevronUpIcon";
import DocumentTextIcon from "@heroicons/vue/24/outline/DocumentTextIcon";
import MagnifyingGlassIcon from "@heroicons/vue/24/outline/MagnifyingGlassIcon";
import PlusIcon from "@heroicons/vue/24/outline/PlusIcon";
import ViewColumnsIcon from "@heroicons/vue/24/outline/ViewColumnsIcon";
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import AppLayout from "../components/AppLayout.vue";
import AddProjectDialog from "../components/projects/AddProjectDialog.vue";
import BoardHeaderInfo from "../components/projects/BoardHeaderInfo.vue";
import BoardMoreMenu from "../components/projects/BoardMoreMenu.vue";
import BoardStateFilter from "../components/projects/BoardStateFilter.vue";
import GitHubSignInBanner from "../components/projects/GitHubSignInBanner.vue";
import JobCard from "../components/projects/JobCard.vue";
import KanbanBoard from "../components/projects/KanbanBoard.vue";
import ProjectChatPanel from "../components/projects/ProjectChatPanel.vue";
import ProjectLogsPanel from "../components/projects/ProjectLogsPanel.vue";
import ProjectModeMenu from "../components/projects/ProjectModeMenu.vue";
import ProjectNavRow from "../components/projects/ProjectNavRow.vue";
import QueueDialog from "../components/projects/QueueDialog.vue";
import QueueMenu from "../components/projects/QueueMenu.vue";
import ScheduledCard from "../components/projects/ScheduledCard.vue";
import StatChipRow, {
	type StatChip,
} from "../components/projects/StatChipRow.vue";
import UpNextStrip from "../components/projects/UpNextStrip.vue";
import HeaderOverflow from "../components/ui/HeaderOverflow.vue";
import { useAgentItemDialogs } from "../composables/useAgentItemDialogs";
import { useFilterRowFit } from "../composables/useFilterRowFit";
import {
	type HeaderCollapseItem,
	useHeaderCollapse,
} from "../composables/useHeaderCollapse";
import {
	JOBS_PAGE_SIZE,
	useProjectBoard,
	useProjectList,
	useProjectSchedules,
} from "../composables/useProjects";
import { useTaskPause } from "../composables/useTaskPause";
import { filterCounts as countFilters } from "../lib/boardFilterRow";
import {
	HEADER_ADD_CLS,
	HEADER_COLLAPSE_CLS,
	HEADER_COLLAPSE_ICON_CLS,
	HEADER_COLLAPSE_LABEL_CLS,
	HEADER_ICON_CLS,
	HEADER_ICON_ON_CLS,
} from "../lib/headerActions";
import {
	agentProjectsSettingsPath,
	projectOwner,
	projectSettingsPath,
	projectTargets,
} from "../lib/projectSettings";
import {
	BOARD_STATES,
	type BoardFilter,
	type BoardState,
	boardItems,
	jobStates,
	type ProjectJob,
	type ProjectSchedule,
	parseBoardStates,
	projectPath,
	type QueueSkill,
	scheduleJob,
	scheduleMatches,
	stateLabel,
	issueSummary as summarizeIssues,
	summarizePrompt,
	targetURL,
} from "../lib/projects";
import { taskSessionPath, useSettingsStore } from "../stores/settings";

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
// The agent whose Code Loop this board sits under; a watch no agent
// owns sits under System instead.
const routeAgent = computed(() =>
	typeof route.params.agent === "string" ? route.params.agent : "",
);
// Without a project the route is the agent's Code Loop: one board of
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
// A job's details open under the header, in place of the board, so the
// header, its panels and the board's filters stay put.
const jobOpen = computed(() => route.matched.length > 1);
// The selected filters narrow the board; none shows everything. They live
// in the URL as ?state=scheduled,running, so a link opens the same filter,
// and each board remembers its own in localStorage, so leaving the board,
// opening a job and reloading keep them. A URL's filters win over remembered
// ones. Scheduled selects the project's scheduled items; the rest select
// jobs by state, and a scheduled task's run is a job like any other.
const filters = computed(() => parseBoardStates(route.query.state));
const states = computed(() => jobStates(filters.value));
// Selecting only Scheduled hides every job.
const showJobs = computed(() => !filters.value.length || !!states.value.length);
const showSchedules = computed(
	() => !filters.value.length || filters.value.includes("scheduled"),
);
const statesKey = computed(
	() =>
		`aviary.projects.states.${project.value || `agent:${routeAgent.value}`}`,
);
function setStates(next: BoardFilter[]) {
	const state = parseBoardStates(next.join(",")).join(",");
	if (state) localStorage.setItem(statesKey.value, state);
	else localStorage.removeItem(statesKey.value);
	void router.replace({ query: { ...route.query, state: state || undefined } });
}
// The toolbar's layout follows its own width (#1490).
const toolbar = ref<HTMLElement | null>(null);
const stateFilter = ref<{ naturalWidth: number } | null>(null);
const kanbanButton = ref<HTMLElement | null>(null);
const toolbarCount = ref<HTMLElement | null>(null);
const { stacked: toolbarStacked, collapsed: filtersCollapsed } =
	useFilterRowFit(
		toolbar,
		computed(() => stateFilter.value?.naturalWidth ?? 0),
		[kanbanButton, toolbarCount],
	);
function toggleState(state: BoardFilter) {
	setStates(
		filters.value.includes(state)
			? filters.value.filter((s) => s !== state)
			: [...filters.value, state],
	);
}
watch(
	() => [statesKey.value, route.query.state, route.query.tab, route.query.view],
	() => {
		// The board replaced the old Schedules view: its links land on the
		// board's scheduled items.
		if (route.query.tab === "schedules" || route.query.view === "schedules") {
			void router.replace({ query: { state: "scheduled" } });
			return;
		}
		if (typeof route.query.state === "string") {
			const state = filters.value.join(",");
			if (state) localStorage.setItem(statesKey.value, state);
			else localStorage.removeItem(statesKey.value);
			return;
		}
		const saved = parseBoardStates(localStorage.getItem(statesKey.value));
		if (saved.length)
			void router.replace({
				query: { ...route.query, state: saved.join(",") },
			});
	},
	{ immediate: true },
);
// The search narrows the jobs within the filter, across every field and the
// whole list rather than one page.
const search = ref("");
const searching = computed(() => search.value.trim() !== "");
// The kanban layout survives reloads, like the prompt chat's open state.
const KANBAN_KEY = "aviary.projects.kanban";
const kanban = ref(localStorage.getItem(KANBAN_KEY) === "1");
watch(kanban, (on) => localStorage.setItem(KANBAN_KEY, on ? "1" : "0"));
const {
	status,
	jobs,
	total,
	unfilteredTotal,
	page,
	error,
	loading,
	refresh,
	checkNow,
	signInToGitHub,
	switchToPolling,
	importProfile,
	retry,
	stop,
	dismiss,
	clear,
	scheduleEntries,
} = useProjectBoard(targets, states, search, kanban, ref(true));
// The header's issue filter is editable for one project (#1213): the
// project's own board, or a board of a single project such as a watch's.
const filterTarget = computed(() =>
	targets.value.length === 1 ? targets.value[0] : "",
);
// A manual project (#1307) is never searched for issues, so its header says
// so in place of the issue filter.
const manual = computed(
	() =>
		!!filterTarget.value &&
		!!status.value?.targets.find((t) => t.name === filterTarget.value)?.manual,
);
// Open issue counts for the board's projects, as a header chip (#1434).
const issueSummary = computed(() =>
	summarizeIssues(status.value?.targets ?? [], targets.value),
);
const {
	schedules,
	upcoming,
	agents: scheduleAgents,
	error: scheduleError,
	runNow: runScheduleNow,
	refresh: refreshSchedules,
} = useProjectSchedules(project, targets);
// The projects' scheduled tasks are their own kind of box on the board, one
// per task whatever its runs: in the list and in the kanban's Scheduled
// column, while the Scheduled chip or no chip is selected, or a selected
// job state is the task's own: Running while a run is in flight, Failed
// while its last run failed.
function scheduleInFilter(s: ProjectSchedule) {
	if (showSchedules.value) return true;
	const state = scheduleJob(scheduleEntries.value, s.id)?.schedule?.state;
	return !!state && state !== "scheduled" && states.value.includes(state);
}
const boardSchedules = computed(() =>
	schedules.value.filter(
		(s) => scheduleInFilter(s) && scheduleMatches(s, search.value),
	),
);
const boardJobs = computed(() => (showJobs.value ? jobs.value : []));
// The list interleaves scheduled cards with the jobs by when they next run,
// re-sorting as the clock ticks; the kanban's Scheduled column keeps that
// order.
const boardList = computed(() =>
	boardItems(
		boardSchedules.value,
		scheduleEntries.value,
		boardJobs.value,
		now.value,
	),
);
const boardListSchedules = computed(() =>
	boardList.value.flatMap((i) => (i.kind === "schedule" ? [i.schedule] : [])),
);
// A scheduled box's footer runs, pauses and resumes its task in place, and
// opens its settings in the shared task dialog, as the sidebar's gear does.
const busySchedule = ref("");
async function onScheduleAction(
	fn: (s: ProjectSchedule) => Promise<void>,
	s: ProjectSchedule,
) {
	busySchedule.value = s.id;
	try {
		await fn(s);
	} catch (e) {
		error.value = e instanceof Error ? e.message : String(e);
	} finally {
		busySchedule.value = "";
	}
}
// Pause and Resume flip the box at once, as the sidebar does, and flip it
// back with a toast when the change fails.
async function toggleSchedule(s: ProjectSchedule) {
	const enabled = !s.enabled;
	s.enabled = enabled;
	if (!(await setTaskEnabled(s.agent, s.name, enabled))) s.enabled = !enabled;
	await refreshSchedules();
}
function openScheduleSettings(s: ProjectSchedule) {
	openItemDialog({ kind: "task", mode: "edit", agent: s.agent, name: s.name });
}
// Duplicate opens the create form prefilled from the schedule's task, as
// the sidebar's Duplicate does, and refreshes the board once it is saved.
function duplicateSchedule(s: ProjectSchedule) {
	openItemDialog({
		kind: "task",
		mode: "create",
		agent: s.agent,
		duplicate: s.name,
		onCreated: () => void refreshSchedules(),
	});
}
// An Up next run opens its task's page, as the task's card does.
function openSchedule(id: string) {
	const s = schedules.value.find((t) => t.id === id);
	if (s) void router.push(taskSessionPath(s.agent, s.name));
}

// The agent a new scheduled task runs under: the project's owner, or the
// first agent when no agent owns the watch.
const scheduleAgent = computed(
	() => owner.value || scheduleAgents.value[0] || "",
);
// Schedule creates a scheduled task the way any is created, through the
// shared Add task dialog, as a "project" task targeting this project.
const { open: openItemDialog } = useAgentItemDialogs();
const { setTaskEnabled } = useTaskPause();
function onNewSchedule() {
	if (!scheduleAgent.value) return;
	openItemDialog({
		kind: "task",
		mode: "create",
		agent: scheduleAgent.value,
		project: project.value,
		onCreated: () => void refreshSchedules(),
	});
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
// Every job on the board, whatever the selected states.
const allJobs = computed(() =>
	BOARD_STATES.reduce((n, s) => n + (status.value?.states[s] ?? 0), 0),
);
// The board counts its scheduled items with its jobs: "items" once the
// project has any.
const noun = computed(() => (schedules.value.length ? "item" : "job"));
// A schedule that is running or failed counts in its job state too, but is
// one item.
const scheduleJobStates = computed(
	() =>
		scheduleEntries.value.filter(
			(e) => e.schedule && e.schedule.state !== "scheduled",
		).length,
);
const allItems = computed(
	() =>
		Math.max(allJobs.value - scheduleJobStates.value, 0) +
		schedules.value.length,
);
const shownTotal = computed(
	() => (showJobs.value ? total.value : 0) + boardSchedules.value.length,
);
const unfilteredShown = computed(
	() =>
		(showJobs.value ? unfilteredTotal.value : 0) +
		schedules.value.filter(scheduleInFilter).length,
);
function chipCount(state: BoardFilter): number | null {
	if (state === "scheduled") return schedules.value.length;
	return status.value ? status.value.states[state] : null;
}
const filterCounts = computed(() => countFilters(chipCount));

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

// Each card also has a short chip form for the compact row.
const statCards = computed<StatChip[]>(() => {
	const s = status.value;
	if (!s) return [];
	return [
		{
			label: "Slots",
			value: s.slotsUsed,
			suffix: `of ${s.slotsTotal} busy`,
			chip: `${s.slotsUsed}/${s.slotsTotal}`,
			chipLabel: "slots",
			cls: "border-gray-200 text-gray-900 dark:border-gray-800 dark:text-white",
		},
		...BOARD_STATES.map((state) => ({
			label: stateLabel(state),
			value: s.states[state],
			chip: String(s.states[state]),
			chipLabel: stateLabel(state).toLowerCase(),
			cls: STATE_CARD[state],
		})),
		{
			label: "Merged (24h)",
			value: s.merged24h,
			chip: String(s.merged24h),
			chipLabel: "merged 24h",
			cls: "border-purple-200 bg-purple-50 text-purple-700 dark:border-purple-900 dark:bg-purple-950/40 dark:text-purple-300",
		},
	];
});
// The counts default to the compact row below md and to the cards from md
// up, following the viewport until the user picks one; the pick survives
// reloads.
const STATS_COMPACT_KEY = "aviary.projects.statsCompact";
const savedStats = localStorage.getItem(STATS_COMPACT_KEY);
const statsChoice = ref<boolean | null>(
	savedStats === null ? null : savedStats === "1",
);
watch(statsChoice, (compact) => {
	if (compact !== null)
		localStorage.setItem(STATS_COMPACT_KEY, compact ? "1" : "0");
});
const wideQuery = window.matchMedia("(min-width: 768px)");
const wide = ref(wideQuery.matches);
function onWideChange(e: MediaQueryListEvent) {
	wide.value = e.matches;
}
onMounted(() => wideQuery.addEventListener("change", onWideChange));
onUnmounted(() => wideQuery.removeEventListener("change", onWideChange));
// Tailwind's sm breakpoint, where the board's header actions fit beside an
// open job's (#1347).
const smQuery = window.matchMedia("(min-width: 640px)");
const smUp = ref(smQuery.matches);
function onSmChange(e: MediaQueryListEvent) {
	smUp.value = e.matches;
}
onMounted(() => smQuery.addEventListener("change", onSmChange));
onUnmounted(() => smQuery.removeEventListener("change", onSmChange));
const statsCompact = computed(() => statsChoice.value ?? !wide.value);

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

// The Queue menu's picker (#1095).
const queueSkill = ref<QueueSkill | null>(null);
const queueTarget = ref("");
function openQueue(skill: QueueSkill, target: string) {
	queueTarget.value = target;
	queueSkill.value = skill;
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

// Logs moves into the "…" menu on a header row under 600px, then Watch
// under 420px (a phone), and either sooner if the row would not fit (#1488).
// On a phone the compact header is one line of chips and actions (#1576),
// so Chat, Schedule, Queue and Add project follow, then the details and
// instance icons, until the line fits; desktop keeps them all.
const moreMenu = ref<{ trigger: HTMLElement | null } | null>(null);
// A watch session's board is one agent's, so it queues when it has one project.
const canQueue = computed(() => !aggregate.value || targets.value.length === 1);
const singleLineHeader = computed(() => !smUp.value && !jobOpen.value);
const collapseItems = computed((): HeaderCollapseItem[] => {
	const items: HeaderCollapseItem[] = [{ id: "logs-toggle", below: 600 }];
	if (filterTarget.value) items.push({ id: "project-mode-menu", below: 420 });
	if (!singleLineHeader.value) return items;
	items.push({ id: "prompt-chat-toggle" });
	if (!aggregate.value) items.push({ id: "schedule-new" });
	if (canQueue.value) items.push({ id: "queue-menu" });
	if (aggregate.value) items.push({ id: "board-add-project" });
	if (status.value) items.push({ id: "board-info" });
	if (status.value?.instanceID) items.push({ id: "instance-tag" });
	return items;
});
const headerCollapsed = useHeaderCollapse(
	computed(() => moreMenu.value?.trigger),
	() => collapseItems.value,
	{
		singleLine: () => singleLineHeader.value,
		deps: [
			aggregate,
			targets,
			smUp,
			manual,
			() => issueSummary.value?.label,
			() => online.value,
		],
	},
);
// The items in the "…" menu instead of the header, never in both (#1486).
const inMenu = computed(
	() =>
		new Set(
			collapseItems.value.slice(0, headerCollapsed.value).map(({ id }) => id),
		),
);
const logsInMenu = computed(() => inMenu.value.has("logs-toggle"));
const watchInMenu = computed(() => inMenu.value.has("project-mode-menu"));

async function onSummarize(job: ProjectJob) {
	chatOpen.value = true;
	await nextTick();
	await chatPanel.value?.send(summarizePrompt(job));
}
</script>
