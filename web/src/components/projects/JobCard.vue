<template>
	<article data-testid="job-card"
		class="flex min-w-0 flex-col overflow-hidden rounded-xl border border-gray-200 bg-white dark:border-gray-800 dark:bg-gray-900">
		<div class="space-y-1.5 p-4">
			<div class="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs">
				<JobStatusPill :state="job.state" />
				<router-link v-if="showProject" :to="projectBoardPath" data-testid="job-project"
					class="truncate rounded-full bg-gray-100 px-2 py-0.5 font-mono text-gray-700 hover:bg-gray-200 dark:bg-gray-800 dark:text-gray-200 dark:hover:bg-gray-700"
					:title="`Open ${job.target}'s board`">{{ job.target }}</router-link>
				<span v-if="job.scheduled" data-testid="job-scheduled"
					class="rounded-full bg-violet-50 px-2 py-0.5 font-medium text-violet-700 dark:bg-violet-950/50 dark:text-violet-300">
					Scheduled
				</span>
				<span v-if="job.startedAt" class="text-gray-400">{{ age(job.startedAt, now) }}</span>
				<span class="ml-auto truncate font-mono text-gray-500 dark:text-gray-400">
					{{ [job.skill, job.cli].filter(Boolean).join(" · ") }}
				</span>
			</div>
			<h3 class="text-base font-semibold leading-snug text-gray-900 dark:text-white">
				<router-link :to="runPath" class="hover:underline">
					{{ job.number && !job.scheduled ? `#${job.number} ` : "" }}{{ job.title || job.id }}
				</router-link>
			</h3>
			<p v-if="job.scheduled" class="text-xs text-gray-500 dark:text-gray-400">
				Scheduled prompt<template v-if="job.scheduleID"> · <span class="font-mono">{{ job.scheduleID }}</span></template>
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
		<pre data-testid="job-output"
			class="h-40 flex-1 overflow-hidden whitespace-pre-wrap break-all bg-slate-950 px-4 py-3 font-mono text-xs leading-relaxed text-slate-200">{{ outputText }}</pre>
		<div class="flex flex-wrap items-center gap-2 border-t border-gray-100 px-3 py-2.5 dark:border-gray-800">
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
			<button v-if="job.number && job.repo" type="button" data-testid="job-summarize"
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
			<span v-if="job.cloneDir" class="min-w-0 flex-1 truncate text-right font-mono text-xs text-gray-400"
				:title="job.cloneDir">{{ job.cloneDir }}</span>
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
import { useSettingsStore } from "../../stores/settings";
import JobStatusPill from "./JobStatusPill.vue";
import PullRequestLink from "./PullRequestLink.vue";

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

const waitingText = computed(() => waitText(props.job, props.now));

const progressPct = computed(() =>
	Math.round((props.job.progressDone / props.job.progressTotal) * 100),
);

const outputText = computed(() =>
	props.job.output.length
		? props.job.output.slice(-12).join("\n")
		: "No output yet.",
);
</script>
