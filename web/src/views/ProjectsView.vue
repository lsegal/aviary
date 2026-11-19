<template>
	<AppLayout>
		<div class="flex h-full flex-col overflow-y-auto">
			<div v-if="!project" class="flex flex-1 items-center justify-center p-8">
				<div class="max-w-md text-center">
					<h1 class="text-xl font-semibold text-gray-900 dark:text-white">Projects</h1>
					<p v-if="!projectList.available.value" class="mt-2 text-sm text-gray-500 dark:text-gray-400">
						Projects isn't running on this server.
					</p>
					<p v-else class="mt-2 text-sm text-gray-500 dark:text-gray-400">
						No projects are watched yet. Run <code class="font-mono">aviary watch owner/repo</code> to start one.
					</p>
				</div>
			</div>
			<template v-else>
				<header
					class="flex flex-col gap-3 border-b border-gray-200 px-4 py-4 dark:border-gray-800 sm:px-6 xl:flex-row xl:items-center">
					<div class="min-w-0">
						<p class="text-xs text-gray-500"><router-link to="/projects" class="hover:underline">Projects</router-link> /</p>
						<div class="flex items-center gap-2">
							<h1 class="truncate text-xl font-bold text-gray-900 dark:text-white">{{ project }}</h1>
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
						<div role="tablist" class="flex overflow-hidden rounded-lg border border-gray-200 text-sm dark:border-gray-700">
							<button v-for="f in FILTERS" :key="f.key" type="button" role="tab" :aria-selected="filter === f.key"
								class="px-3 py-1.5" :class="filter === f.key ? 'bg-blue-600 text-white' : 'text-gray-600 hover:bg-gray-50 dark:text-gray-300 dark:hover:bg-gray-800'"
								@click="filter = f.key">
								{{ f.key === "all" && total ? `${f.label} ${total}` : f.label }}
							</button>
						</div>
						<button type="button"
							class="rounded-lg border border-gray-200 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800"
							:disabled="checking" @click="onCheckNow">
							{{ checking ? "Checking…" : "Check now" }}
						</button>
					</div>
				</header>

				<div class="space-y-4 px-4 py-4 sm:px-6">
					<p v-if="status && !status.running" class="rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-800 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-300">
						The watch isn't running{{ status.error ? `: ${status.error}` : "." }}
					</p>
					<p v-else-if="status?.error" class="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900 dark:bg-red-950/40 dark:text-red-300">
						{{ status.error }}
					</p>
					<p v-if="error" class="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900 dark:bg-red-950/40 dark:text-red-300">
						{{ error }}
					</p>

					<section v-if="status" class="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5" aria-label="Job counts">
						<div v-for="s in statCards" :key="s.label" class="rounded-xl border px-4 py-3" :class="s.cls">
							<p class="text-xs font-semibold uppercase tracking-wide opacity-80">{{ s.label }}</p>
							<p class="text-2xl font-bold">{{ s.value }}<span v-if="s.suffix" class="ml-1 text-sm font-normal opacity-70">{{ s.suffix }}</span></p>
						</div>
					</section>

					<section v-if="status?.clis.length" class="flex flex-wrap items-center gap-2 text-xs" aria-label="Coding CLIs">
						<span class="font-medium text-gray-600 dark:text-gray-300">Coding CLIs</span>
						<span v-for="cli in status.clis" :key="cli.name"
							class="inline-flex items-center gap-1.5 rounded-full border border-gray-200 px-2.5 py-1 dark:border-gray-700">
							<span class="h-1.5 w-1.5 rounded-full" :class="cli.signedIn ? 'bg-green-500' : 'bg-gray-400'" />
							<b class="text-gray-800 dark:text-gray-100">{{ cli.name }}</b>
							<span class="text-gray-500">{{ cli.quota || "no quota reported" }}</span>
						</span>
					</section>

					<div v-if="loading && !jobs.length" class="py-12 text-center text-sm text-gray-400">Loading jobs…</div>
					<div v-else-if="!jobs.length" class="py-12 text-center text-sm text-gray-400">No jobs here yet.</div>
					<section v-else class="grid gap-4 md:grid-cols-2 2xl:grid-cols-3" aria-label="Jobs">
						<JobCard v-for="job in jobs" :key="job.id" :job="job" :now="now"
							:busy="busyJob === job.id" @retry="onAction(retry, $event)" @stop="onAction(stop, $event)" />
					</section>

					<nav v-if="pageCount > 1" class="flex items-center justify-center gap-3 text-sm" aria-label="Job pages">
						<button type="button" class="rounded-lg border border-gray-200 px-3 py-1.5 disabled:opacity-40 dark:border-gray-700"
							:disabled="page === 0" @click="page--">Previous</button>
						<span class="text-gray-500">Page {{ page + 1 }} of {{ pageCount }}</span>
						<button type="button" class="rounded-lg border border-gray-200 px-3 py-1.5 disabled:opacity-40 dark:border-gray-700"
							:disabled="page + 1 >= pageCount" @click="page++">Next</button>
					</nav>

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
						<span v-if="version">Version {{ version }}</span>
					</footer>
				</div>
			</template>
		</div>
	</AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import AppLayout from "../components/AppLayout.vue";
import JobCard from "../components/projects/JobCard.vue";
import {
	JOBS_PAGE_SIZE,
	useProjectBoard,
	useProjectList,
} from "../composables/useProjects";
import { useServerStatus } from "../composables/useServerStatus";
import {
	age,
	type BoardFilter,
	encodeProject,
	type ProjectJob,
} from "../lib/projects";

const FILTERS: { key: BoardFilter; label: string }[] = [
	{ key: "all", label: "All" },
	{ key: "running", label: "Running" },
	{ key: "waiting", label: "Waiting" },
	{ key: "done", label: "Done" },
	{ key: "failed", label: "Failed" },
];

const route = useRoute();
const router = useRouter();
const projectList = useProjectList();
const project = computed(() =>
	typeof route.params.project === "string" ? route.params.project : "",
);
const filter = ref<BoardFilter>("all");
const { version } = useServerStatus();
const { status, jobs, total, page, error, loading, checkNow, retry, stop } =
	useProjectBoard(project, filter);

// Without a project in the URL, open the first watched project.
watch(
	() => projectList.projects.value,
	(list) => {
		if (!project.value && list.length > 0) {
			void router.replace(`/projects/${encodeProject(list[0].name)}`);
		}
	},
);

const now = ref(Date.now());
let clock: ReturnType<typeof setInterval> | undefined;
onMounted(() => {
	clock = setInterval(() => {
		now.value = Date.now();
	}, 1000);
});
onUnmounted(() => clearInterval(clock));

const online = computed(() => status.value?.deliveryOnline ?? true);
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
			label: "Completed",
			value: s.completed,
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
</script>
