<template>
	<AppLayout>
		<div class="flex h-full min-h-0">
			<div class="flex h-full min-w-0 flex-1 flex-col">
			<div class="flex min-h-0 flex-1 flex-col overflow-y-auto">
				<template v-if="project || aggregate">
					<header
						class="flex shrink-0 flex-col gap-3 border-b border-gray-200 px-4 py-4 dark:border-gray-800 sm:px-6 xl:flex-row xl:items-center">
						<div class="min-w-0">
							<p v-if="aggregate" class="text-xs text-gray-500">{{ routeAgent }} /</p>
							<p v-else class="text-xs text-gray-500"><router-link :to="projectsIndexPath" class="hover:underline">{{ projectsIndexLabel }}</router-link> /</p>
							<div class="flex flex-wrap items-center gap-x-2 gap-y-1">
								<h1 v-if="aggregate" class="truncate text-xl font-bold text-gray-900 dark:text-white">Code Loop</h1>
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
								<template v-if="status.issueFilter">Issues matching <code data-testid="issue-filter" class="font-mono">{{ status.issueFilter }}</code></template>
								<template v-else>All open issues</template>
								<template v-if="status.delivery"> · {{ status.delivery }}</template>
								<template v-if="status.lastChecked"> · checked {{ age(status.lastChecked, now) }} ago</template>
								<span v-if="status.stale" data-testid="stale-indicator" class="text-gray-400 dark:text-gray-500"
									title="Showing the state saved before the last restart until the first live poll confirms it">
									· refreshing…<template v-if="status.cachedAt"> last updated {{ age(status.cachedAt, now) }} ago</template>
								</span>
								<template v-if="status.interval"> · every {{ status.interval }}</template>
							</p>
						</div>
						<div class="flex flex-wrap items-center gap-2 xl:ml-auto">
							<button v-if="aggregate" type="button" data-testid="board-add-project" aria-label="Add project" title="Add project"
								class="rounded-lg border border-blue-600 bg-blue-600 p-1.5 text-white hover:bg-blue-700"
								@click="openAdd">
								<PlusIcon aria-hidden="true" class="h-5 w-5" />
							</button>
							<button v-if="!aggregate" type="button" data-testid="schedule-new" title="Schedule a task for this project"
								class="inline-flex items-center gap-1.5 rounded-lg border border-gray-200 px-2.5 py-1.5 text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800"
								:disabled="!scheduleAgent" @click="onNewSchedule">
								<CalendarDaysIcon aria-hidden="true" class="h-5 w-5" />
								Schedule
							</button>
							<button type="button" aria-label="Check now" title="Check now"
								class="rounded-lg border border-gray-200 p-1.5 text-gray-700 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800"
								:disabled="checking" @click="onCheckNow">
								<ArrowPathIcon aria-hidden="true" class="h-5 w-5" :class="{ 'animate-spin': checking }" />
							</button>
							<button type="button" data-testid="logs-toggle" :aria-pressed="logsOpen"
								aria-label="Logs" title="Logs"
								class="rounded-lg border p-1.5"
								:class="logsOpen ? 'border-blue-600 bg-blue-600 text-white' : 'border-gray-200 text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800'"
								@click="logsOpen = !logsOpen">
								<DocumentTextIcon aria-hidden="true" class="h-5 w-5" />
							</button>
							<button type="button" data-testid="prompt-chat-toggle" :aria-pressed="chatOpen"
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

						<UpNextStrip v-if="!aggregate" :runs="upcoming" :now="now" @open="openSchedule" />

						<!-- The counts show as full cards or as one compact row of chips, the
							default below md, and a toggle switches between them. -->
						<div v-if="status" class="flex min-w-0 gap-2" :class="statsCompact ? 'items-center' : 'items-start'">
							<!-- Columns follow the width the board actually has, not the viewport, so
								the cards reflow onto more rows instead of squeezing their values. -->
							<section v-if="!statsCompact" id="job-counts" class="grid min-w-0 flex-1 grid-cols-[repeat(auto-fill,minmax(9.5rem,1fr))] gap-3" aria-label="Job counts">
								<div v-for="s in statCards" :key="s.label" class="min-w-0 rounded-xl border px-4 py-3" :class="s.cls" data-testid="stat-card">
									<p class="truncate text-xs font-semibold uppercase tracking-wide opacity-80" :title="s.label" data-testid="stat-label">{{ s.label }}</p>
									<p class="truncate whitespace-nowrap text-2xl font-bold" data-testid="stat-value">{{ s.value }}<span v-if="s.suffix" class="ml-1 text-sm font-normal opacity-70">{{ s.suffix }}</span></p>
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

						<section v-if="status?.clis.length" class="flex flex-wrap items-center gap-2 text-xs" aria-label="CLI providers">
							<span class="font-medium text-gray-600 dark:text-gray-300">CLI providers</span>
							<span v-for="cli in status.clis" :key="cli.name"
								class="inline-flex max-w-full flex-wrap items-center gap-x-1.5 rounded-full border border-gray-200 px-2.5 py-1 dark:border-gray-700">
								<span class="h-1.5 w-1.5 rounded-full" :class="cli.signedIn ? 'bg-green-500' : 'bg-gray-400'" />
								<b class="text-gray-800 dark:text-gray-100">{{ cli.name }}</b>
								<span v-if="cli.models.length" class="text-gray-600 dark:text-gray-300">{{ cli.models.join(", ") }}</span>
								<span class="text-gray-500">{{ cli.quota || "no quota reported" }}</span>
							</span>
						</section>

						<div class="flex flex-wrap items-center gap-3 text-sm" data-testid="job-toolbar">
							<div class="relative min-w-0 flex-1 max-sm:basis-full sm:max-w-xs">
								<span class="pointer-events-none absolute inset-y-0 left-3 flex items-center text-gray-400 dark:text-gray-500"
									aria-hidden="true" data-testid="search-icon">
									<MagnifyingGlassIcon class="h-4 w-4" />
								</span>
								<input v-model="search" type="search" aria-label="Search jobs" placeholder="Search jobs, logs, #issue, #PR, session…"
									class="field-input pl-9!" />
							</div>
								<!-- Below md the chips fold into a Filter button; md and up shows them inline as before. -->
								<div ref="filterMenu" class="relative min-w-0 md:contents" @keydown.esc="closeFilter(true)">
									<button ref="filterToggle" type="button" data-testid="filter-toggle" aria-controls="job-filter"
										:aria-expanded="filterOpen"
										class="flex max-w-full items-center gap-1.5 rounded-lg border px-2.5 py-1 md:hidden"
										:class="filters.length ? 'border-blue-600 text-blue-700 dark:border-blue-400 dark:text-blue-300' : 'border-gray-300 text-gray-600 hover:bg-gray-50 dark:border-gray-600 dark:text-gray-300 dark:hover:bg-gray-800'"
										@click="filterOpen = !filterOpen">
										<FunnelIcon aria-hidden="true" class="h-4 w-4 shrink-0" />
										<span class="truncate" data-testid="filter-summary">Filter: {{ filterSummary }}</span>
										<ChevronDownIcon aria-hidden="true" class="h-4 w-4 shrink-0 transition-transform" :class="filterOpen && 'rotate-180'" />
									</button>
									<div id="job-filter" role="group" aria-label="Job filter" class="max-w-full flex-wrap items-center gap-1.5 md:flex"
										:class="filterOpen ? 'max-md:absolute max-md:left-0 max-md:top-full max-md:z-20 max-md:mt-1 max-md:flex max-md:w-64 max-md:rounded-xl max-md:border max-md:border-gray-200 max-md:bg-white max-md:p-2 max-md:shadow-lg dark:max-md:border-gray-700 dark:max-md:bg-gray-900' : 'max-md:hidden'">
										<button v-for="state in filterChips" :key="state" type="button" :aria-pressed="filters.includes(state)"
											class="shrink-0 rounded-full border px-3 py-1" :class="filters.includes(state) ? FILTER_ACTIVE[state] : 'border-gray-300 text-gray-600 hover:bg-gray-50 dark:border-gray-600 dark:text-gray-300 dark:hover:bg-gray-800'"
											@click="toggleState(state)">
											{{ stateLabel(state) }}<span v-if="chipCount(state) !== null" class="ml-1.5 tabular-nums opacity-75">{{ chipCount(state) }}</span>
										</button>
										<button v-if="filters.length" type="button" class="px-1 text-gray-500 hover:text-gray-800 hover:underline dark:text-gray-400 dark:hover:text-gray-100"
											@click="setStates([])">Clear</button>
									</div>
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
									{{ shownTotal }} of {{ unfilteredShown }} {{ unfilteredShown === 1 ? `${noun} matches` : `${noun}s match` }}
								</span>
								<span v-else class="text-gray-500 dark:text-gray-400" data-testid="job-total">
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

						<section v-if="detailSchedule" data-testid="schedule-detail" :aria-label="`Schedule ${detailSchedule.name}`"
							class="space-y-4 rounded-xl border border-violet-200 bg-white p-4 dark:border-violet-900 dark:bg-gray-900">
							<header class="flex flex-wrap items-center gap-2">
								<router-link :to="boardQuery" aria-label="Back to board" title="Back to board"
									class="rounded-lg border border-gray-200 p-1.5 text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800">
									<ArrowLeftIcon aria-hidden="true" class="h-4 w-4" />
								</router-link>
								<span class="rounded-full bg-violet-50 px-2 py-0.5 text-xs font-medium text-violet-700 dark:bg-violet-950/50 dark:text-violet-300">Scheduled</span>
								<h2 class="truncate text-lg font-semibold text-gray-900 dark:text-white">{{ detailSchedule.name }}</h2>
							</header>
							<p v-if="scheduleError" class="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900 dark:bg-red-950/40 dark:text-red-300">
								{{ scheduleError }}
							</p>
							<ScheduleEditor :key="detailSchedule.id" :schedule="detailSchedule" :save="saveSchedule"
								:remove="removeSchedule" :run-now="runScheduleNow" :preview="previewSchedule"
								@deleted="router.replace(boardQuery)" />
						</section>

						<template v-else>
						<div v-if="loading && !boardJobs.length && !boardSchedules.length" class="py-12 text-center text-sm text-gray-400">Loading jobs…</div>
						<KanbanBoard v-else-if="kanban" :jobs="boardJobs" :filters="filters" :now="now" :schedules="boardSchedules"
							:schedule-to="aggregate ? undefined : scheduleTo" />
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
							<ScheduledCard v-for="s in boardSchedules" :key="s.id" :schedule="s" :now="now" :to="scheduleTo(s)"
								:job="scheduleJob(jobs, s.id)" />
							<JobCard v-for="job in boardJobs" :key="job.id" :job="job" :now="now" :show-project="aggregate"
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
import ArrowLeftIcon from "@heroicons/vue/24/outline/ArrowLeftIcon";
import ArrowPathIcon from "@heroicons/vue/24/outline/ArrowPathIcon";
import CalendarDaysIcon from "@heroicons/vue/24/outline/CalendarDaysIcon";
import ChatBubbleLeftRightIcon from "@heroicons/vue/24/outline/ChatBubbleLeftRightIcon";
import ChevronDownIcon from "@heroicons/vue/24/outline/ChevronDownIcon";
import ChevronLeftIcon from "@heroicons/vue/24/outline/ChevronLeftIcon";
import ChevronRightIcon from "@heroicons/vue/24/outline/ChevronRightIcon";
import ChevronUpIcon from "@heroicons/vue/24/outline/ChevronUpIcon";
import Cog6ToothIcon from "@heroicons/vue/24/outline/Cog6ToothIcon";
import DocumentTextIcon from "@heroicons/vue/24/outline/DocumentTextIcon";
import FunnelIcon from "@heroicons/vue/24/outline/FunnelIcon";
import MagnifyingGlassIcon from "@heroicons/vue/24/outline/MagnifyingGlassIcon";
import PlusIcon from "@heroicons/vue/24/outline/PlusIcon";
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
import ScheduledCard from "../components/projects/ScheduledCard.vue";
import ScheduleEditor from "../components/projects/ScheduleEditor.vue";
import StatChipRow, {
	type StatChip,
} from "../components/projects/StatChipRow.vue";
import UpNextStrip from "../components/projects/UpNextStrip.vue";
import { useAgentItemDialogs } from "../composables/useAgentItemDialogs";
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
	BOARD_FILTERS,
	BOARD_STATES,
	type BoardFilter,
	type BoardState,
	jobStates,
	type ProjectJob,
	type ProjectSchedule,
	parseBoardStates,
	projectPath,
	scheduleJob,
	scheduleMatches,
	stateLabel,
	summarizePrompt,
	targetURL,
} from "../lib/projects";
import { shortVersion } from "../lib/version";
import { useSettingsStore } from "../stores/settings";

// A selected state chip fills with that state's color; Starting is orange.
const FILTER_ACTIVE: Record<BoardFilter, string> = {
	scheduled: "border-violet-600 bg-violet-600 text-white",
	starting: "border-orange-500 bg-orange-500 text-white",
	paused: "border-amber-500 bg-amber-500 text-white",
	running: "border-blue-600 bg-blue-600 text-white",
	done: "border-green-600 bg-green-600 text-white",
	failed: "border-red-600 bg-red-600 text-white",
};

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
const projectsIndexPath = computed(() =>
	routeAgent.value
		? `/agents/${encodeURIComponent(routeAgent.value)}/projects`
		: route.path,
);
const projectsIndexLabel = computed(() =>
	routeAgent.value ? `${routeAgent.value} · Code Loop` : "Watches",
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
	() =>
		!aggregate.value &&
		(!filters.value.length || filters.value.includes("scheduled")),
);
// A board of agent projects has no scheduled items, so no Scheduled chip.
const filterChips = computed(() =>
	aggregate.value ? BOARD_STATES : BOARD_FILTERS,
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
// Below md the chips fold into a Filter button whose label sums up the
// selection; its panel closes on Esc or a tap outside it.
const filterOpen = ref(false);
const filterMenu = ref<HTMLElement | null>(null);
const filterToggle = ref<HTMLButtonElement | null>(null);
const filterSummary = computed(() => {
	const active = filterChips.value.filter((s) => filters.value.includes(s));
	if (!active.length) return "All";
	return active
		.map((s) => {
			const n = chipCount(s);
			return n === null ? stateLabel(s) : `${stateLabel(s)} ${n}`;
		})
		.join(", ");
});
function closeFilter(focus = false) {
	if (!filterOpen.value) return;
	filterOpen.value = false;
	if (focus) filterToggle.value?.focus();
}
function onFilterOutside(e: PointerEvent) {
	if (filterMenu.value && !filterMenu.value.contains(e.target as Node))
		closeFilter();
}
onMounted(() => document.addEventListener("pointerdown", onFilterOutside));
onUnmounted(() => document.removeEventListener("pointerdown", onFilterOutside));
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
		// board's scheduled items, with the linked schedule's editor open.
		if (route.query.tab === "schedules" || route.query.view === "schedules") {
			const { schedule } = route.query;
			void router.replace({
				query: {
					state: "scheduled",
					scheduled:
						typeof schedule === "string" && schedule ? schedule : undefined,
				},
			});
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
} = useProjectBoard(targets, states, search, kanban);
const {
	schedules,
	upcoming,
	agents: scheduleAgents,
	error: scheduleError,
	save: saveSchedule,
	remove: removeSchedule,
	runNow: runScheduleNow,
	preview: previewSchedule,
	refresh: refreshSchedules,
} = useProjectSchedules(project);
// The project's scheduled tasks are their own kind of box on the board: in
// the list and in the kanban's Scheduled column, while the Scheduled chip or
// no chip is selected.
const boardSchedules = computed(() =>
	showSchedules.value
		? schedules.value.filter((s) => scheduleMatches(s, search.value))
		: [],
);
const boardJobs = computed(() => (showJobs.value ? jobs.value : []));
// A scheduled box opens its editor on the board, at ?scheduled=<id>, so a
// reload or a shared link opens the same place.
function scheduleTo(s: ProjectSchedule) {
	return { query: { ...boardQuery.value.query, scheduled: s.id } };
}
const boardQuery = computed(() => {
	const { scheduled: _, ...query } = route.query;
	return { query };
});
const detailSchedule = computed(() => {
	const id = route.query.scheduled;
	return typeof id === "string"
		? (schedules.value.find((s) => s.id === id) ?? null)
		: null;
});
function openSchedule(id: string) {
	void router.push({ query: { ...route.query, scheduled: id } });
}

// The agent a new scheduled task runs under: the project's owner, or the
// first agent when no agent owns the watch.
const scheduleAgent = computed(
	() => owner.value || scheduleAgents.value[0] || "",
);
// Schedule creates a scheduled task the way any is created, through the
// shared Add task dialog, as a "project" task targeting this project.
const { open: openItemDialog } = useAgentItemDialogs();
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
const allItems = computed(
	() => allJobs.value + (aggregate.value ? 0 : schedules.value.length),
);
const shownTotal = computed(
	() => (showJobs.value ? total.value : 0) + boardSchedules.value.length,
);
const unfilteredShown = computed(
	() =>
		(showJobs.value ? unfilteredTotal.value : 0) +
		(showSchedules.value ? schedules.value.length : 0),
);
function chipCount(state: BoardFilter): number | null {
	if (state === "scheduled") return schedules.value.length;
	return status.value ? status.value.states[state] : null;
}

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
