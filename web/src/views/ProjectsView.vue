<template>
	<AppLayout>
		<div class="flex h-full min-h-0">
			<div class="flex h-full min-w-0 flex-1 flex-col overflow-y-auto">
				<template v-if="project || aggregate">
					<header
						class="flex flex-col gap-3 border-b border-gray-200 px-4 py-4 dark:border-gray-800 sm:px-6 xl:flex-row xl:items-center">
						<div class="min-w-0">
							<p v-if="aggregate" class="text-xs text-gray-500">{{ routeAgent }} /</p>
							<p v-else class="text-xs text-gray-500"><router-link :to="projectsIndexPath" class="hover:underline">{{ projectsIndexLabel }}</router-link> /</p>
							<div class="flex items-center gap-2">
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
							</div>
							<p v-if="status" class="text-xs text-gray-500 dark:text-gray-400">
								Issues labeled <b>{{ status.readyLabel }}</b>
								<template v-if="status.delivery"> · {{ status.delivery }}</template>
								<template v-if="status.lastChecked"> · checked {{ age(status.lastChecked, now) }} ago</template>
								<template v-if="status.interval"> · every {{ status.interval }}</template>
							</p>
						</div>
						<div class="flex flex-wrap items-center gap-2 xl:ml-auto">
							<div v-if="!aggregate" role="tablist" aria-label="Project views" class="flex overflow-hidden rounded-lg border border-gray-200 text-sm dark:border-gray-700">
								<button v-for="v in VIEWS" :key="v.key" type="button" role="tab" :aria-selected="view === v.key"
									class="px-3 py-1.5" :class="view === v.key ? 'bg-gray-900 text-white dark:bg-white dark:text-gray-900' : 'text-gray-600 hover:bg-gray-50 dark:text-gray-300 dark:hover:bg-gray-800'"
									@click="setView(v.key)">
									{{ v.key === "schedules" && schedules.length ? `${v.label} ${schedules.length}` : v.label }}
								</button>
							</div>
							<div v-if="view === 'board'" role="tablist" aria-label="Job filter" class="flex overflow-hidden rounded-lg border border-gray-200 text-sm dark:border-gray-700">
								<button v-for="f in FILTERS" :key="f.key" type="button" role="tab" :aria-selected="filter === f.key"
									class="px-3 py-1.5" :class="filter === f.key ? 'bg-blue-600 text-white' : 'text-gray-600 hover:bg-gray-50 dark:text-gray-300 dark:hover:bg-gray-800'"
									@click="filter = f.key">
									{{ f.key === "all" && total ? `${f.label} ${total}` : f.label }}
								</button>
							</div>
							<button v-if="view === 'board'" type="button"
								class="rounded-lg border border-gray-200 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800"
								:disabled="checking" @click="onCheckNow">
								{{ checking ? "Checking…" : "Check now" }}
							</button>
							<router-link :to="settingsPath" data-testid="project-settings-link"
								class="rounded-lg border border-gray-200 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800">
								Settings
							</router-link>
							<button v-if="view === 'board'" type="button" data-testid="prompt-chat-toggle" :aria-pressed="chatOpen"
								class="rounded-lg border px-3 py-1.5 text-sm"
								:class="chatOpen ? 'border-blue-600 bg-blue-600 text-white' : 'border-gray-200 text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800'"
								@click="chatOpen = !chatOpen">
								Prompt chat
							</button>
							<button type="button" :class="ADD_BTN" @click="openAdd">+ Add project</button>
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

						<SchedulesPanel v-if="view === 'schedules'" v-model:selected="selectedSchedule" :project="project"
							:schedules="schedules" :agents="owner ? [owner] : scheduleAgents" :error="scheduleError" :loaded="schedulesLoaded" :now="now"
							:save="saveSchedule" :set-enabled="setScheduleEnabled" :remove="removeSchedule" :run-now="runScheduleNow"
							:preview="previewSchedule" />

						<template v-else>
						<UpNextStrip v-if="!aggregate" :runs="upcoming" :now="now" @open="openSchedule" />

						<section v-if="status" class="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5" aria-label="Job counts">
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
								<span class="text-gray-500">{{ cli.quota || "no quota reported" }}</span>
							</span>
						</section>

						<div class="flex flex-wrap items-center gap-3 text-sm" data-testid="job-toolbar">
							<div class="relative min-w-0 flex-1 sm:max-w-xs">
								<span class="pointer-events-none absolute inset-y-0 left-3 flex items-center text-gray-400 dark:text-gray-500"
									aria-hidden="true" data-testid="search-icon">
									<MagnifyingGlassIcon class="h-4 w-4" />
								</span>
								<input v-model="search" type="search" aria-label="Search jobs" placeholder="Search jobs, logs, #issue, #PR, session…"
									class="field-input pl-9!" />
							</div>
							<span v-if="searching" class="text-gray-500 dark:text-gray-400" data-testid="search-count">
								{{ total }} of {{ unfilteredTotal }} {{ unfilteredTotal === 1 ? "job matches" : "jobs match" }}
							</span>
							<nav v-if="searching || pageCount > 1" class="flex items-center gap-3 sm:ml-auto" aria-label="Job pages">
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

						<div v-if="loading && !jobs.length" class="py-12 text-center text-sm text-gray-400">Loading jobs…</div>
						<div v-else-if="!jobs.length && searching" class="py-12 text-center text-sm text-gray-500 dark:text-gray-400" data-testid="search-empty">
							<p>No jobs match “<b class="text-gray-800 dark:text-gray-100">{{ search.trim() }}</b>”.</p>
							<button type="button" class="mt-3 rounded-lg border border-gray-200 px-3 py-1.5 text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800"
								@click="search = ''">Clear search</button>
						</div>
						<div v-else-if="aggregate && !targets.length" class="py-12 text-center text-sm text-gray-400" data-testid="board-no-projects">
							{{ routeAgent }} owns no projects yet.
							<button type="button" class="text-blue-600 hover:underline dark:text-blue-400" @click="openAdd">Add one</button>.
						</div>
						<div v-else-if="!jobs.length" class="py-12 text-center text-sm text-gray-400">No jobs here yet.</div>
						<section v-else class="grid gap-4 md:grid-cols-2 2xl:grid-cols-3" aria-label="Jobs">
							<JobCard v-for="job in jobs" :key="job.id" :job="job" :now="now" :show-project="aggregate"
								:busy="busyJob === job.id" @retry="onAction(retry, $event)" @stop="onAction(stop, $event)"
								@summarize="onSummarize" />
						</section>
						</template>

						<details v-if="status" class="rounded-xl border border-gray-200 bg-gray-50 dark:border-gray-800 dark:bg-gray-900">
							<summary class="flex cursor-pointer items-center gap-3 px-4 py-3 text-sm">
								<b class="text-gray-800 dark:text-gray-100">Project log</b>
								<span class="min-w-0 flex-1 truncate font-mono text-xs text-gray-500">{{ status.log.at(-1) ?? "" }}</span>
							</summary>
							<pre class="max-h-64 overflow-auto border-t border-gray-200 px-4 py-3 font-mono text-xs text-gray-600 dark:border-gray-800 dark:text-gray-300">{{ status.log.join("\n") || "No log lines yet." }}</pre>
						</details>

						<footer v-if="status" class="flex flex-wrap gap-x-4 gap-y-1 text-xs text-gray-500 dark:text-gray-400">
							<span v-if="status.instanceID">Instance <code class="font-mono">{{ status.instanceID }}</code></span>
							<span v-if="status.targets.length">
								Targets:
								<template v-for="(t, i) in status.targets" :key="t.name">{{ i ? ", " : "" }}{{ t.name }} ({{ t.issues }})</template>
							</span>
							<span v-if="version">Version {{ shortVersion(version) }}</span>
						</footer>
					</div>
				</template>
			</div>
			<ProjectChatPanel v-if="(project || aggregate) && chatOpen" ref="chatPanel" :project="project" @close="chatOpen = false" />
		</div>
		<AddProjectDialog v-if="adding" :agent="routeAgent" @close="closeAdd" @added="onAdded" />
	</AppLayout>
</template>

<script setup lang="ts">
import ChevronLeftIcon from "@heroicons/vue/24/outline/ChevronLeftIcon";
import ChevronRightIcon from "@heroicons/vue/24/outline/ChevronRightIcon";
import MagnifyingGlassIcon from "@heroicons/vue/24/outline/MagnifyingGlassIcon";
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import AppLayout from "../components/AppLayout.vue";
import AddProjectDialog from "../components/projects/AddProjectDialog.vue";
import GitHubSignInBanner from "../components/projects/GitHubSignInBanner.vue";
import JobCard from "../components/projects/JobCard.vue";
import ProjectChatPanel from "../components/projects/ProjectChatPanel.vue";
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
	type BoardFilter,
	type ProjectJob,
	projectPath,
	summarizePrompt,
	targetURL,
} from "../lib/projects";
import { shortVersion } from "../lib/version";
import { useSettingsStore } from "../stores/settings";

const FILTERS: { key: BoardFilter; label: string }[] = [
	{ key: "all", label: "All" },
	{ key: "running", label: "Running" },
	{ key: "waiting", label: "Waiting" },
	{ key: "done", label: "Done" },
	{ key: "failed", label: "Failed" },
];

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
} = useProjectBoard(targets, filter, search);
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
} = useProjectSchedules(project);

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

const ADD_BTN =
	"rounded-lg bg-blue-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-blue-700";
// The add-project dialog is open while the URL carries ?add=1, so the
// sidebar's "Add project" link can open it from anywhere.
const adding = computed(() => route.query.add === "1");
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

const statCards = computed(() => {
	const s = status.value;
	if (!s) return [];
	return [
		{
			label: "Slots",
			value: s.slotsUsed,
			suffix: `of ${s.slotsTotal} busy`,
			cls: "border-gray-200 text-gray-900 dark:border-gray-800 dark:text-white",
		},
		{
			label: "Running",
			value: s.activeJobs,
			cls: "border-blue-200 bg-blue-50 text-blue-700 dark:border-blue-900 dark:bg-blue-950/40 dark:text-blue-300",
		},
		{
			label: "Waiting",
			value: s.waitingJobs,
			cls: "border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-300",
		},
		{
			label: "Merged (24h)",
			value: s.merged24h,
			cls: "border-green-200 bg-green-50 text-green-700 dark:border-green-900 dark:bg-green-950/40 dark:text-green-300",
		},
		{
			label: "Failed",
			value: s.failed,
			cls: "border-red-200 bg-red-50 text-red-700 dark:border-red-900 dark:bg-red-950/40 dark:text-red-300",
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
const chatPanel = ref<{ send: (text: string) => Promise<void> } | null>(null);

async function onSummarize(job: ProjectJob) {
	chatOpen.value = true;
	await nextTick();
	await chatPanel.value?.send(summarizePrompt(job));
}
</script>
