<template>
	<div class="flex min-h-0 flex-1 flex-col" data-testid="job-detail">
		<!-- The job's status, links and actions sit in the shared page header
			(#1147), whose title is the job's crumb. -->
		<Teleport defer to="#page-header-status">
			<JobStatusPill v-if="job" :state="job.state" />
			<span v-if="job?.scheduled"
				class="rounded-full bg-violet-50 px-2 py-0.5 text-xs font-medium text-violet-700 dark:bg-violet-950/50 dark:text-violet-300">
				Scheduled
			</span>
		</Teleport>
		<Teleport defer to="#page-header-subtitle">
			<p data-testid="job-detail-title" class="min-w-0 break-words">
				<template v-if="!job?.scheduled">
					<a v-if="issueURL" :href="issueURL" target="_blank" rel="noreferrer" data-testid="job-detail-issue-link"
						:title="`Open issue #${number} on GitHub`" class="text-blue-600 hover:underline dark:text-blue-400">#{{ number }}</a>
					<template v-else>#{{ number }}</template>
					<template v-if="job?.pr"> •
						<PullRequestLink :job="job" data-testid="job-detail-pr-link" :title="`Open PR ${prLabel} on GitHub`"
							class="align-baseline text-blue-600 dark:text-blue-400" /></template>{{ " " }}
				</template>{{ job?.title ?? "" }}
			</p>
		</Teleport>
		<Teleport defer to="#page-header-actions">
			<div data-testid="job-detail-actions" class="flex shrink-0 items-center gap-2">
				<template v-if="job">
					<button type="button" data-testid="raw-output-toggle" :aria-pressed="showRaw" aria-label="Raw output"
						:title="showRaw ? 'Hide the raw CLI output' : 'Show the raw CLI output'"
						:class="showRaw ? HEADER_ICON_ON_CLS : HEADER_ICON_CLS" @click="showRaw = !showRaw">
						<CodeBracketIcon aria-hidden="true" class="h-5 w-5" />
					</button>
					<button type="button" :disabled="busy || !job.actions.retry"
						aria-label="Retry" :title="job.actions.retryUnavailable || 'Retry this job'" :class="HEADER_ICON_CLS"
						@click="run(retry)"><ArrowPathIcon aria-hidden="true" class="h-5 w-5" /></button>
					<button v-if="stoppable" type="button" :disabled="busy" aria-label="Stop" title="Stop this job"
						:class="[HEADER_ICON_CLS, 'text-red-600! dark:text-red-400!']"
						@click="run(stop)"><StopIcon aria-hidden="true" class="h-5 w-5" /></button>
				</template>
				<router-link :to="boardPath" data-testid="job-detail-close" aria-label="Close details" title="Close details"
					class="inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-full text-gray-400 hover:bg-gray-100 hover:text-gray-700 dark:text-gray-500 dark:hover:bg-gray-800 dark:hover:text-gray-200"><XMarkIcon aria-hidden="true" class="h-5 w-5" /></router-link>
				<span aria-hidden="true" class="h-5 w-px shrink-0 bg-gray-200 dark:bg-gray-700" />
			</div>
		</Teleport>

		<dl v-if="job" data-testid="job-meta"
			class="grid grid-cols-[auto_minmax(0,1fr)] items-center gap-x-3 gap-y-1 border-b border-gray-200 px-4 py-2 text-xs dark:border-gray-800 sm:px-6">
			<template v-if="job.session">
				<dt class="text-gray-500">session</dt>
				<dd data-testid="job-session" class="min-w-0 select-all truncate font-mono text-gray-700 dark:text-gray-300">{{ job.session }}</dd>
			</template>
			<template v-if="agentLabel">
				<dt class="text-gray-500">agent</dt>
				<dd data-testid="job-agent" class="min-w-0 truncate font-mono text-gray-700 dark:text-gray-300">{{ agentLabel }}</dd>
			</template>
			<template v-if="job.cli && job.modelSource">
				<dt class="text-gray-500">model</dt>
				<dd data-testid="job-model" class="min-w-0 truncate text-gray-700 dark:text-gray-300"><span class="font-mono">{{ job.cli }}</span> ({{ job.modelSource }})</dd>
			</template>
			<template v-if="!job.scheduled">
				<dt class="text-gray-500">clone</dt>
				<dd data-testid="job-clone" class="flex min-w-0 items-start gap-2">
					<template v-if="job.cloneState === 'present'">
						<code data-testid="job-clone-path" class="min-w-0 select-all font-mono [overflow-wrap:anywhere] text-gray-700 dark:text-gray-300"
							:title="job.cloneDir">{{ job.cloneDir }}</code>
						<button type="button" :aria-label="copied ? 'Copied clone path' : 'Copy clone path'" title="Copy the clone path"
							class="flex shrink-0 items-center gap-1 rounded-md border border-gray-200 px-2 py-0.5 text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800"
							@click="copyClonePath">
							<ClipboardDocumentIcon aria-hidden="true" class="h-3.5 w-3.5" />{{ copied ? "Copied" : "Copy" }}
						</button>
					</template>
					<span v-else class="min-w-0 text-gray-500 [overflow-wrap:anywhere]">
						{{ cloneMessage }}
						<code v-if="job.cloneDir" class="select-all font-mono">{{ job.cloneDir }}</code>
					</span>
				</dd>
			</template>
		</dl>

		<div v-if="detail && !job?.scheduled" class="@container border-b border-gray-200 px-4 py-3 dark:border-gray-800 sm:px-6">
			<p data-testid="lifecycle-compact" class="mb-2 hidden text-xs font-medium text-gray-700 @max-4xl:block dark:text-gray-300">
				{{ compactLabel }}
			</p>
			<ol data-testid="lifecycle" aria-label="Lifecycle" class="flex min-w-0 items-center gap-2 overflow-hidden text-sm">
				<li v-for="(label, i) in LIFECYCLE_STEPS" :key="label" :data-step="stepState(i)"
					class="relative flex min-w-0 items-center gap-2" :class="i < LIFECYCLE_STEPS.length - 1 ? 'flex-1' : 'shrink-0 @4xl:shrink'"
					:title="`Step ${i + 1} of ${LIFECYCLE_STEPS.length}: ${label} (${STEP_TEXT[stepState(i)]})`"
					:aria-label="`${label}, ${STEP_TEXT[stepState(i)]}`"
					:aria-current="stepState(i) === 'current' || stepState(i) === 'failed' ? 'step' : undefined">
					<span class="flex h-6 w-6 shrink-0 items-center justify-center rounded-full text-xs font-semibold"
						:class="STEP_CLASS[stepState(i)]">
						<template v-if="stepState(i) === 'done'">✓</template>
						<XMarkIcon v-else-if="stepState(i) === 'failed'" aria-hidden="true" class="h-3.5 w-3.5" />
						<template v-else>{{ i + 1 }}</template>
					</span>
					<span class="min-w-0 truncate @max-4xl:sr-only"
						:class="stepState(i) === 'current' || stepState(i) === 'failed' ? 'font-semibold text-gray-900 dark:text-white' : 'text-gray-600 dark:text-gray-400'">
						{{ label }}
					</span>
					<span v-if="i < LIFECYCLE_STEPS.length - 1" aria-hidden="true"
						class="h-px min-w-3 flex-1 border-t-2"
						:class="i < lifecycle.step || lifecycle.done ? 'border-solid border-green-600' : 'border-dashed border-gray-300 dark:border-gray-700'" />
				</li>
			</ol>
		</div>

		<p v-if="error" class="mx-4 mt-3 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900 dark:bg-red-950/40 dark:text-red-300">
			{{ error }}
		</p>

		<div class="relative flex min-h-0 flex-1 flex-col overflow-y-auto lg:flex-row lg:overflow-hidden">
			<section class="flex min-w-0 shrink-0 flex-col lg:min-h-0 lg:flex-1 lg:shrink" aria-label="Timeline">
				<div class="relative flex min-w-0 flex-1 flex-col lg:min-h-0">
					<div ref="transcriptEl" data-testid="job-transcript"
						class="relative min-w-0 flex-1 px-4 py-4 sm:px-6 lg:min-h-0 lg:overflow-y-auto" @scroll="transcript.onScroll">
						<div v-if="!detail && !error" class="py-12 text-center text-sm text-gray-400">Loading…</div>
						<JobTimeline v-else-if="detail" :events="detail.events" />
					</div>
					<MoreBelowPill v-if="transcript.showMore.value" :count="transcript.newCount.value"
						data-testid="job-transcript-more-below" @click="transcript.scrollToBottom()" />
				</div>
				<footer v-if="job && !job.scheduled"
					class="flex flex-col gap-3 border-t border-gray-200 px-4 py-3 text-sm text-gray-600 dark:border-gray-800 dark:text-gray-300 sm:px-6 2xl:flex-row 2xl:items-center">
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

			<section v-if="showRaw" data-testid="raw-output"
				class="flex h-80 shrink-0 flex-col bg-slate-950 text-slate-200 lg:h-auto lg:w-[40%]" aria-label="Output">
				<div class="flex items-center gap-3 border-b border-slate-800 px-4 py-2 text-xs">
					<b class="text-sm text-white">Output</b>
					<label class="ml-auto flex shrink-0 items-center gap-1 text-slate-400">
						<input v-model="showHeartbeats" data-testid="raw-output-heartbeats" type="checkbox" class="h-3 w-3" /> heartbeats
					</label>
					<button type="button" aria-label="Close raw output" title="Hide the raw CLI output"
						class="rounded p-0.5 text-slate-400 hover:bg-slate-800 hover:text-white" @click="showRaw = false">
						<XMarkIcon aria-hidden="true" class="h-4 w-4" />
					</button>
				</div>
				<div class="relative flex min-h-0 flex-1 flex-col">
					<pre ref="outputEl" data-testid="run-output"
						class="min-h-0 flex-1 overflow-auto whitespace-pre-wrap break-all px-4 py-3 font-mono text-xs leading-relaxed"
						@scroll="output.onScroll">{{ outputLines.join("\n") || "No output yet." }}</pre>
					<MoreBelowPill v-if="output.showMore.value" :count="output.newCount.value"
						data-testid="run-output-more-below" @click="output.scrollToBottom()" />
				</div>
			</section>
		</div>
	</div>
</template>

<script setup lang="ts">
import ArrowPathIcon from "@heroicons/vue/24/outline/ArrowPathIcon";
import ClipboardDocumentIcon from "@heroicons/vue/24/outline/ClipboardDocumentIcon";
import CodeBracketIcon from "@heroicons/vue/24/outline/CodeBracketIcon";
import StopIcon from "@heroicons/vue/24/outline/StopIcon";
import XMarkIcon from "@heroicons/vue/24/outline/XMarkIcon";
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useAutoFollow } from "../../composables/useAutoFollow";
import { useProjectJob } from "../../composables/useProjects";
import { HEADER_ICON_CLS, HEADER_ICON_ON_CLS } from "../../lib/headerActions";
import {
	githubURL,
	isHeartbeatLine,
	LIFECYCLE_STEPS,
	lifecycleStep,
	projectPath,
} from "../../lib/projects";
import MoreBelowPill from "../MoreBelowPill.vue";
import JobStatusPill from "./JobStatusPill.vue";
import JobTimeline from "./JobTimeline.vue";
import PullRequestLink from "./PullRequestLink.vue";

const route = useRoute();
const router = useRouter();
const project = computed(() => String(route.params.project ?? ""));
const number = computed(() => Number(route.params.number ?? 0));
const agent = computed(() => String(route.params.agent ?? ""));
// Job details open inside the project page, so the way back is the board
// under the same header.
const boardPath = computed(() => projectPath(agent.value, project.value));

// Escape closes the pane like the close button, unless something on the page
// (a menu, a dialog, a text field) already handled it.
function onKeydown(e: KeyboardEvent) {
	if (e.key !== "Escape" || e.defaultPrevented) return;
	const target = e.target as HTMLElement | null;
	if (
		target?.closest(
			"input:not([type=checkbox]):not([type=radio]), textarea, select, [contenteditable=true], [role=dialog]",
		)
	)
		return;
	router.push(boardPath.value);
}
onMounted(() => window.addEventListener("keydown", onKeydown));
onBeforeUnmount(() => window.removeEventListener("keydown", onKeydown));

const { detail, error, allowedCommenters, retry, stop } = useProjectJob(
	project,
	number,
);
const job = computed(() => detail.value?.job ?? null);

// The Clone row: the path with a copy button, or why there is none.
const cloneMessage = computed(() => {
	const current = job.value;
	if (!current) return "";
	switch (current.cloneState) {
		case "reused":
			return current.cloneReusedBy
				? `This job's clone was handed to #${current.cloneReusedBy}:`
				: "This job's clone was handed to a later job:";
		case "removed":
			return "This job's clone was removed:";
		default:
			return "No clone yet.";
	}
});
const copied = ref(false);
let copiedTimer: ReturnType<typeof setTimeout> | undefined;
async function copyClonePath() {
	if (!job.value?.cloneDir) return;
	try {
		await navigator.clipboard.writeText(job.value.cloneDir);
	} catch {
		return;
	}
	copied.value = true;
	clearTimeout(copiedTimer);
	copiedTimer = setTimeout(() => {
		copied.value = false;
	}, 2000);
}
const lifecycle = computed(() =>
	detail.value ? lifecycleStep(detail.value) : { step: 0, done: false },
);
type StepState = "done" | "current" | "failed" | "pending";
const STEP_CLASS: Record<StepState, string> = {
	done: "bg-green-600 text-white",
	current:
		"bg-blue-600 text-white ring-2 ring-blue-300 ring-offset-1 dark:ring-blue-800 dark:ring-offset-gray-900",
	failed:
		"bg-red-600 text-white ring-2 ring-red-300 ring-offset-1 dark:ring-red-800 dark:ring-offset-gray-900",
	pending:
		"border border-dashed border-gray-400 text-gray-500 dark:border-gray-600",
};
const STEP_TEXT: Record<StepState, string> = {
	done: "done",
	current: "current",
	failed: "failed",
	pending: "pending",
};
function stepState(i: number): StepState {
	const { step, done } = lifecycle.value;
	if (done || i < step) return "done";
	if (i > step) return "pending";
	return job.value?.state === "failed" ? "failed" : "current";
}
// The stepper's narrow form: only the current step's label, like "5/6 · CI".
const compactLabel = computed(() => {
	const { step, done } = lifecycle.value;
	const total = LIFECYCLE_STEPS.length;
	if (done) return `${total}/${total} · ${LIFECYCLE_STEPS[total - 1]} ✓`;
	const failed = stepState(step) === "failed" ? " (failed)" : "";
	return `${step + 1}/${total} · ${LIFECYCLE_STEPS[step]}${failed}`;
});
const agentLabel = computed(() =>
	[job.value?.cli, job.value?.skill].filter(Boolean).join(" · "),
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
const prLabel = computed(() =>
	job.value?.pr
		? `${job.value.prRepo !== job.value.repo ? job.value.prRepo : ""}#${job.value.pr}`
		: "",
);

// The raw CLI output is noisy, so it stays closed until asked for, and the
// choice is remembered.
const RAW_OUTPUT_KEY = "aviary.projects.jobRawOutput";
const showRaw = ref(localStorage.getItem(RAW_OUTPUT_KEY) === "1");
watch(showRaw, (open) =>
	localStorage.setItem(RAW_OUTPUT_KEY, open ? "1" : "0"),
);
const showHeartbeats = ref(false);
const outputLines = computed(() => {
	const output = detail.value?.output ?? [];
	return showHeartbeats.value
		? output
		: output.filter((line) => !isHeartbeatLine(line));
});

// The transcript and the raw output each follow their newest entry.
const transcriptEl = ref<HTMLElement | null>(null);
const transcript = useAutoFollow(transcriptEl, {
	count: () => detail.value?.events.length ?? 0,
});
const outputEl = ref<HTMLElement | null>(null);
const output = useAutoFollow(outputEl, {
	count: () => outputLines.value.length,
});
// Another job opens at its newest entry.
watch([agent, number], () => {
	transcript.scrollToBottom({ smooth: false });
	output.scrollToBottom({ smooth: false });
});

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
