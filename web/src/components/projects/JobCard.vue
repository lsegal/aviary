<template>
	<article data-testid="job-card"
		class="flex max-h-[50vh] min-w-0 flex-col overflow-hidden rounded-xl border border-gray-200 bg-white dark:border-gray-800 dark:bg-gray-900">
		<div class="shrink-0 space-y-1.5 p-4">
			<div class="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs">
				<JobStatusPill :state="job.state" />
				<router-link v-if="showProject" :to="projectBoardPath" data-testid="job-project"
					class="truncate rounded-full bg-gray-100 px-2 py-0.5 font-mono text-gray-700 hover:bg-gray-200 dark:bg-gray-800 dark:text-gray-200 dark:hover:bg-gray-700"
					:title="`Open ${job.target}'s board`">{{ job.target }}</router-link>
				<span v-if="job.scheduled" data-testid="job-scheduled"
					class="rounded-full bg-violet-50 px-2 py-0.5 font-medium text-violet-700 dark:bg-violet-950/50 dark:text-violet-300">
					Scheduled
				</span>
				<span v-if="job.startedAt" data-testid="job-time" class="ml-auto whitespace-nowrap text-gray-400"
					:title="`Started ${new Date(job.startedAt).toLocaleString()}`">{{ age(job.startedAt, now) }}</span>
			</div>
			<h3 class="text-base font-semibold leading-snug text-gray-900 dark:text-white">
				<router-link :to="runPath" class="hover:underline">
					{{ job.number && !job.scheduled ? `#${job.number} ` : "" }}{{ job.title || job.id }}
				</router-link>
			</h3>
			<p v-if="job.scheduled" class="text-xs text-gray-500 dark:text-gray-400">
				Scheduled prompt<template v-if="scheduleTask"> · <span data-testid="job-schedule-task" class="whitespace-nowrap font-mono"
					:title="job.scheduleID">{{ scheduleTask }}</span></template>
			</p>
			<p v-else class="flex flex-wrap items-center gap-x-1 text-xs text-gray-500 dark:text-gray-400">
				<a v-if="job.number && job.repo" :href="job.issueURL || githubURL(job.repo, 'issues', job.number)"
					target="_blank" rel="noreferrer" class="hover:underline">#{{ job.number }}</a>
				<template v-if="job.pr">
					<span>·</span>
					<PullRequestLink :job="job" />
				</template>
				<template v-else-if="job.status !== 'queued' && job.number">
					<span>·</span>
					<span>no PR yet</span>
				</template>
			</p>
			<p v-if="waitingText" data-testid="job-waiting" class="text-xs text-amber-700 dark:text-amber-400">
				{{ waitingText }}
			</p>
			<div v-if="job.progressTotal > 0" class="flex items-center gap-2 pt-1"
				:title="`${job.progressDone} of ${job.progressTotal} plan steps done`">
				<div class="h-1.5 flex-1 overflow-hidden rounded-full bg-gray-100 dark:bg-gray-800">
					<div data-testid="job-progress" class="h-full rounded-full bg-blue-500 transition-all"
						:style="{ width: `${progressPct}%` }" />
				</div>
				<span class="text-xs tabular-nums text-gray-500">{{ job.progressDone }}/{{ job.progressTotal }}</span>
			</div>
		</div>
		<JobOutputPane :job="job" />
		<div class="flex shrink-0 flex-wrap items-center gap-2 border-t border-gray-100 px-3 py-2.5 dark:border-gray-800">
			<router-link :to="runPath" aria-label="Details" title="Open this job's run view"
				class="rounded-lg border border-gray-200 p-1.5 text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800">
				<ArrowsPointingOutIcon aria-hidden="true" class="h-4 w-4" />
			</router-link>
			<button type="button" :disabled="busy || !job.actions.retry"
				aria-label="Retry" :title="job.actions.retryUnavailable || 'Retry this job'"
				class="rounded-lg border border-gray-200 p-1.5 text-gray-700 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800"
				@click="emit('retry', job)">
				<ArrowPathIcon aria-hidden="true" class="h-4 w-4" />
			</button>
			<button v-if="job.number && job.repo && !auth.minimal" type="button" data-testid="job-summarize"
				aria-label="Summarize" title="Ask the prompt chat to summarize this job's session log"
				class="rounded-lg border border-gray-200 p-1.5 text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800"
				@click="emit('summarize', job)">
				<SparklesIcon aria-hidden="true" class="h-4 w-4" />
			</button>
			<button v-if="job.actions.stop" type="button" :disabled="busy"
				aria-label="Stop" title="Stop this job"
				class="rounded-lg border border-gray-200 p-1.5 text-gray-700 hover:bg-red-50 hover:text-red-600 disabled:opacity-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-red-950/40"
				@click="emit('stop', job)">
				<StopIcon aria-hidden="true" class="h-4 w-4" />
			</button>
			<JobOutputToggle />
			<div class="flex min-w-0 flex-1 flex-col items-end text-right font-mono text-xs">
				<span v-if="agentLabel" data-testid="job-agent" class="max-w-full truncate text-gray-500 dark:text-gray-400"
					:title="agentLabel">{{ agentLabel }}</span>
				<span v-if="job.cloneDir" data-testid="job-clone" class="max-w-full truncate text-gray-400"
					:title="job.cloneDir">{{ job.cloneDir }}</span>
			</div>
		</div>
	</article>
</template>

<script setup lang="ts">
import ArrowPathIcon from "@heroicons/vue/24/outline/ArrowPathIcon";
import ArrowsPointingOutIcon from "@heroicons/vue/24/outline/ArrowsPointingOutIcon";
import SparklesIcon from "@heroicons/vue/24/outline/SparklesIcon";
import StopIcon from "@heroicons/vue/24/outline/StopIcon";
import { computed } from "vue";
import { projectOwner } from "../../lib/projectSettings";
import {
	age,
	githubURL,
	jobPath,
	type ProjectJob,
	projectPath,
	waitText,
} from "../../lib/projects";
import { useAuthStore } from "../../stores/auth";
import { useSettingsStore } from "../../stores/settings";
import JobOutputPane from "./JobOutputPane.vue";
import JobOutputToggle from "./JobOutputToggle.vue";
import JobStatusPill from "./JobStatusPill.vue";
import PullRequestLink from "./PullRequestLink.vue";

// A watch session has no prompt chat to summarize with.
const auth = useAuthStore();
const props = defineProps<{
	job: ProjectJob;
	now: number;
	busy?: boolean;
	/** Names the job's project, on a board of several. */
	showProject?: boolean;
}>();
const emit = defineEmits<{
	retry: [job: ProjectJob];
	stop: [job: ProjectJob];
	summarize: [job: ProjectJob];
}>();

const settings = useSettingsStore();
const owner = computed(() => projectOwner(settings.config, props.job.target));
const runPath = computed(() =>
	jobPath(owner.value, props.job.target, props.job.number),
);
const projectBoardPath = computed(() =>
	projectPath(owner.value, props.job.target),
);

const agentLabel = computed(() =>
	[props.job.skill, props.job.cli].filter(Boolean).join(" · "),
);

// A scheduled job's task by name, as its title and the task page name it;
// the full "agent/task" ID would wrap at the task name's hyphens.
const scheduleTask = computed(() => {
	const id = props.job.scheduleID;
	return id.slice(id.indexOf("/") + 1);
});

const waitingText = computed(() => waitText(props.job, props.now));

const progressPct = computed(() =>
	Math.round((props.job.progressDone / props.job.progressTotal) * 100),
);
</script>
