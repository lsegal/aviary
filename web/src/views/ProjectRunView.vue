<template>
	<AppLayout>
		<div class="flex h-full min-h-0 flex-col">
			<header class="flex flex-col gap-3 border-b border-gray-200 px-4 py-3 dark:border-gray-800 sm:px-6 lg:flex-row lg:items-center">
				<div class="min-w-0">
					<p class="text-xs text-gray-500">
						<router-link :to="projectsIndexPath" class="text-blue-600 hover:underline">{{ projectsIndexLabel }}</router-link> /
						<router-link :to="boardPath" class="text-blue-600 hover:underline">{{ project }}</router-link> /
					</p>
					<div class="flex flex-wrap items-center gap-2">
						<h1 class="text-lg font-bold text-gray-900 dark:text-white">
							{{ job?.scheduled ? "" : `#${number} ` }}{{ job?.title ?? "" }}
						</h1>
						<JobStatusPill v-if="job" :status="job.status" />
						<span v-if="job?.scheduled"
							class="rounded-full bg-violet-50 px-2 py-0.5 text-xs font-medium text-violet-700 dark:bg-violet-950/50 dark:text-violet-300">
							Scheduled
						</span>
					</div>
				</div>
				<div v-if="job" class="flex flex-wrap items-center gap-2 lg:ml-auto">
					<a v-if="issueURL" :href="issueURL" target="_blank" rel="noreferrer"
						class="rounded-lg border border-gray-200 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800">
						Issue #{{ job.number }} ↗
					</a>
					<a v-if="job.pr" :href="prURL" target="_blank" rel="noreferrer"
						class="rounded-lg border border-gray-200 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800">
						PR {{ job.prRepo !== job.repo ? job.prRepo : "" }}#{{ job.pr }} ↗
					</a>
					<button type="button" :disabled="busy || !job.actions.retry"
						aria-label="Retry" :title="job.actions.retryUnavailable || 'Retry this job'"
						class="rounded-lg border border-gray-200 p-1.5 text-gray-700 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800"
						@click="run(retry)"><ArrowPathIcon aria-hidden="true" class="h-4 w-4" /></button>
					<button v-if="stoppable" type="button" :disabled="busy" aria-label="Stop" title="Stop this job"
						class="rounded-lg border border-red-200 p-1.5 text-red-600 hover:bg-red-50 disabled:opacity-50 dark:border-red-900 dark:hover:bg-red-950/40"
						@click="run(stop)"><StopIcon aria-hidden="true" class="h-4 w-4" /></button>
				</div>
			</header>

			<ol v-if="detail && !job?.scheduled" data-testid="lifecycle"
				class="flex gap-2 overflow-x-auto border-b border-gray-200 px-4 py-3 text-sm dark:border-gray-800 sm:px-6">
				<li v-for="(label, i) in LIFECYCLE_STEPS" :key="label" class="flex shrink-0 items-center gap-2"
					:aria-current="i === lifecycle.step && !lifecycle.done ? 'step' : undefined">
					<span class="flex h-6 w-6 items-center justify-center rounded-full text-xs font-semibold"
						:class="i < lifecycle.step || lifecycle.done ? 'bg-green-600 text-white' : i === lifecycle.step ? 'bg-blue-600 text-white' : 'bg-gray-100 text-gray-500 dark:bg-gray-800'">
						{{ i < lifecycle.step || lifecycle.done ? "✓" : i + 1 }}
					</span>
					<span :class="i === lifecycle.step && !lifecycle.done ? 'font-semibold text-blue-600' : 'text-gray-700 dark:text-gray-300'">{{ label }}</span>
					<span v-if="i < LIFECYCLE_STEPS.length - 1" class="h-px w-6 bg-gray-300 dark:bg-gray-700 xl:w-12" />
				</li>
			</ol>

			<p v-if="error" class="mx-4 mt-3 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900 dark:bg-red-950/40 dark:text-red-300">
				{{ error }}
			</p>

			<div class="flex min-h-0 flex-1 flex-col overflow-y-auto lg:flex-row lg:overflow-hidden">
				<section class="flex shrink-0 flex-col lg:min-h-0 lg:flex-1 lg:shrink" aria-label="Timeline">
					<div class="flex-1 px-4 py-4 sm:px-6 lg:overflow-y-auto">
						<p v-if="job" class="mb-3 text-center text-xs text-gray-500">
							{{ [job.skill, job.cli, job.session ? `session ${job.session}` : ""].filter(Boolean).join(" · ") }}
						</p>
						<div v-if="!detail && !error" class="py-12 text-center text-sm text-gray-400">Loading…</div>
						<JobTimeline v-else-if="detail" :events="detail.events" />
					</div>
					<footer v-if="job && !job.scheduled"
						class="flex flex-col gap-3 border-t border-gray-200 px-4 py-3 text-sm text-gray-600 dark:border-gray-800 dark:text-gray-300 sm:flex-row sm:items-center sm:px-6">
						<p class="flex-1">
							Jobs only take instructions from GitHub comments by allowed commenters<template
								v-if="allowedCommenters.length">
								(<span data-testid="allowed-commenters"><template v-for="(login, i) in allowedCommenters" :key="login">{{ i ? ", " : "" }}<a
										:href="`https://github.com/${encodeURIComponent(login)}`" target="_blank" rel="noreferrer"
										class="font-medium text-blue-600 hover:underline dark:text-blue-400">@{{ login }}</a></template></span>)</template>.
							To steer this job, comment on the issue or its pull request.
						</p>
						<div class="flex flex-wrap gap-2">
							<a v-if="issueURL" :href="`${issueURL}#new_comment_field`"
								target="_blank" rel="noreferrer"
								class="rounded-lg bg-blue-600 px-3 py-1.5 font-medium text-white hover:bg-blue-500">Comment on #{{ job.number }} ↗</a>
							<a v-if="job.pr" :href="`${prURL}#new_comment_field`" target="_blank"
								rel="noreferrer"
								class="rounded-lg border border-gray-200 px-3 py-1.5 hover:bg-gray-50 dark:border-gray-700 dark:hover:bg-gray-800">Comment on PR #{{ job.pr }} ↗</a>
						</div>
					</footer>
				</section>

				<section class="flex h-80 shrink-0 flex-col bg-slate-950 text-slate-200 lg:h-auto lg:w-[40%]" aria-label="Output">
					<div class="flex items-center gap-2 border-b border-slate-800 px-4 py-2 text-xs">
						<b class="text-sm text-white">Output</b>
						<span v-if="job?.cloneDir" class="truncate font-mono text-slate-400">{{ job.cloneDir }}</span>
						<label class="ml-auto flex shrink-0 items-center gap-1 text-slate-400">
							<input v-model="follow" type="checkbox" class="h-3 w-3" /> following
						</label>
					</div>
					<pre ref="outputEl" data-testid="run-output"
						class="flex-1 overflow-auto whitespace-pre-wrap break-all px-4 py-3 font-mono text-xs leading-relaxed">{{ detail?.output.join("\n") || "No output yet." }}</pre>
				</section>
			</div>
		</div>
	</AppLayout>
</template>

<script setup lang="ts">
import ArrowPathIcon from "@heroicons/vue/24/outline/ArrowPathIcon";
import StopIcon from "@heroicons/vue/24/outline/StopIcon";
import { computed, nextTick, ref, watch } from "vue";
import { useRoute } from "vue-router";
import AppLayout from "../components/AppLayout.vue";
import JobStatusPill from "../components/projects/JobStatusPill.vue";
import JobTimeline from "../components/projects/JobTimeline.vue";
import { useProjectJob } from "../composables/useProjects";
import {
	githubURL,
	LIFECYCLE_STEPS,
	lifecycleStep,
	projectPath,
} from "../lib/projects";

const route = useRoute();
const project = computed(() => String(route.params.project ?? ""));
const number = computed(() => Number(route.params.number ?? 0));
const agent = computed(() => String(route.params.agent ?? ""));
const boardPath = computed(() => projectPath(agent.value, project.value));
// A watch no agent owns has no GitHub Projects page above its board.
const projectsIndexPath = computed(() =>
	agent.value
		? `/agents/${encodeURIComponent(agent.value)}/projects`
		: boardPath.value,
);
const projectsIndexLabel = computed(() =>
	agent.value ? `${agent.value} · GitHub Projects` : "Watches",
);

const { detail, error, allowedCommenters, retry, stop } = useProjectJob(
	project,
	number,
);
const job = computed(() => detail.value?.job ?? null);
const lifecycle = computed(() =>
	detail.value ? lifecycleStep(detail.value) : { step: 0, done: false },
);
const stoppable = computed(() => !!job.value?.actions.stop);
const issueURL = computed(() =>
	job.value && !job.value.scheduled
		? job.value.issueURL ||
			githubURL(job.value.repo, "issues", job.value.number)
		: "",
);
const prURL = computed(() =>
	job.value?.pr
		? job.value.prURL || githubURL(job.value.prRepo, "pull", job.value.pr)
		: "",
);

const follow = ref(true);
const outputEl = ref<HTMLElement | null>(null);
watch(
	() => detail.value?.output.length,
	async () => {
		if (!follow.value) return;
		await nextTick();
		if (outputEl.value) outputEl.value.scrollTop = outputEl.value.scrollHeight;
	},
);

const busy = ref(false);
async function run(fn: () => Promise<void>) {
	busy.value = true;
	try {
		await fn();
	} catch (e) {
		error.value = e instanceof Error ? e.message : String(e);
	} finally {
		busy.value = false;
	}
}
</script>
