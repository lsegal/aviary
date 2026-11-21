<template>
	<a :href="job.prURL || githubURL(job.prRepo, 'pull', job.pr)" target="_blank" rel="noreferrer"
		data-testid="job-pr" :data-pr-state="job.prState || undefined" class="inline-flex items-center gap-1 hover:underline">
		<svg data-testid="pr-state-icon" role="img" :aria-label="stateText" viewBox="0 0 16 16" fill="currentColor"
			class="h-3.5 w-3.5 shrink-0" :class="COLORS[job.prState]">
			<title>{{ stateText }}</title>
			<path
				d="M1.5 3.25a2.25 2.25 0 1 1 3 2.122v5.256a2.251 2.251 0 1 1-1.5 0V5.372A2.25 2.25 0 0 1 1.5 3.25Zm5.677-.177L9.573.677A.25.25 0 0 1 10 .854V2.5h1A2.5 2.5 0 0 1 13.5 5v5.628a2.251 2.251 0 1 1-1.5 0V5a1 1 0 0 0-1-1h-1v1.646a.25.25 0 0 1-.427.177L7.177 3.427a.25.25 0 0 1 0-.354ZM3.75 2.5a.75.75 0 1 0 0 1.5.75.75 0 0 0 0-1.5Zm0 9.5a.75.75 0 1 0 0 1.5.75.75 0 0 0 0-1.5Zm8.25.75a.75.75 0 1 0 1.5 0 .75.75 0 0 0-1.5 0Z" />
		</svg>
		<span>PR {{ label }}</span>
	</a>
</template>

<script setup lang="ts">
import { computed } from "vue";
import {
	githubURL,
	type ProjectJob,
	type PullRequestState,
	pullRequestStateLabel,
} from "../../lib/projects";

const props = defineProps<{ job: ProjectJob }>();

// Gray for a draft, green while open, purple once merged.
const COLORS: Record<PullRequestState, string> = {
	draft: "text-gray-400 dark:text-gray-500",
	open: "text-green-600 dark:text-green-400",
	merged: "text-purple-600 dark:text-purple-400",
	"": "text-gray-400 dark:text-gray-500",
};

const stateText = computed(() => pullRequestStateLabel(props.job.prState));

// Fork PRs live in another repository, so name it.
const label = computed(() =>
	props.job.prRepo && props.job.prRepo !== props.job.repo
		? `${props.job.prRepo}#${props.job.pr}`
		: `#${props.job.pr}`,
);
</script>
