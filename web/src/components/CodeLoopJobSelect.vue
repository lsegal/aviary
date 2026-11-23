<template>
	<div>
		<AppCombobox :model-value="jobValue" :options="options" :aria-label="ariaLabel ?? 'Code Loop job'"
			data-testid="codeloop-job-select" :invalid="!!warning"
			:placeholder="loading ? 'Loading Code Loop jobs…' : 'Choose a Code Loop job…'" empty-text="No matching Code Loop jobs"
			@update:model-value="pickJob(String($event))" />
		<p v-if="warning" role="alert" data-testid="codeloop-job-warning"
			class="mt-1 text-xs text-amber-700 dark:text-amber-400">{{ warning }}</p>
		<div v-if="picked && issueOptions" class="mt-2 flex flex-col gap-1">
			<label class="flex items-center gap-2 text-sm text-gray-800 dark:text-gray-200">
				<input type="checkbox" :checked="!!picked.resume" data-testid="codeloop-resume"
					@change="setOption('resume', ($event.target as HTMLInputElement).checked)" />
				Resume the job if it is paused
			</label>
			<label class="flex items-center gap-2 text-sm text-gray-800 dark:text-gray-200">
				<input type="checkbox" :checked="!!picked.reopen" data-testid="codeloop-reopen"
					@change="setOption('reopen', ($event.target as HTMLInputElement).checked)" />
				Reopen the job if it is complete
			</label>
		</div>
	</div>
</template>

<script setup lang="ts">
// A Code Loop job picked as a task's or workflow's output target, shared by
// the task and workflow forms: lists every issue job, Discussion job and
// schedule, grouped by project and searchable by number, title, kind and
// status, and stores its "codeloop:<project>#<number>" target, with the
// resume and reopen delivery options as checkboxes beside it. A schedule is
// stored by its latest run's number, which names the schedule.
import { computed, onMounted } from "vue";
import { useCodeLoopJobs } from "../composables/useCodeLoopJobs";
import type { ProjectJob } from "../lib/projects";
import {
	CODELOOP_TARGET_PREFIX,
	type CodeLoopTargetOptions,
	codeLoopTarget,
	parseCodeLoopTarget,
} from "../lib/taskTargets";
import { AppCombobox, type SelectOption } from "./ui/select";

// Issue jobs are the default and carry no kind label.
function jobKindLabel(job: ProjectJob): string {
	if (job.scheduled) return "schedule";
	return job.skill === "gh-discuss" ? "discussion" : "";
}

const props = defineProps<{ modelValue: string; ariaLabel?: string }>();
const emit = defineEmits<{ "update:modelValue": [value: string] }>();

const { jobs, loaded, loading, load, find } = useCodeLoopJobs();
onMounted(() => void load(true));

// A bare prefix is the target a form starts from before a job is picked.
const value = computed(() =>
	props.modelValue.trim() === CODELOOP_TARGET_PREFIX ? "" : props.modelValue,
);
const picked = computed(() => parseCodeLoopTarget(value.value));
// The combobox lists bare job targets; the options live in the checkboxes.
const jobValue = computed(() =>
	picked.value
		? codeLoopTarget(picked.value.project, picked.value.number)
		: value.value,
);

function pickJob(target: string) {
	const job = parseCodeLoopTarget(target);
	if (!job) {
		emit("update:modelValue", target);
		return;
	}
	emit(
		"update:modelValue",
		codeLoopTarget(job.project, job.number, {
			resume: picked.value?.resume,
			reopen: picked.value?.reopen,
		}),
	);
}

function setOption(option: keyof CodeLoopTargetOptions, on: boolean) {
	if (!picked.value) return;
	const { project, number, resume, reopen } = picked.value;
	emit(
		"update:modelValue",
		codeLoopTarget(project, number, { resume, reopen, [option]: on }),
	);
}
const pickedJob = computed(() =>
	picked.value ? find(picked.value.project, picked.value.number) : undefined,
);
// Resume and reopen only apply to issue jobs: a schedule always takes output
// and a Discussion job is never paused or retried.
const issueOptions = computed(
	() => !pickedJob.value || !jobKindLabel(pickedJob.value),
);
const missing = computed(
	() =>
		!!picked.value &&
		loaded.value &&
		!find(picked.value.project, picked.value.number),
);

const warning = computed(() => {
	if (!value.value.trim()) return "";
	if (!picked.value) return "This target is not a Code Loop job.";
	return missing.value
		? `${picked.value.project}#${picked.value.number} no longer exists; deliveries to it will fail.`
		: "";
});

const options = computed<SelectOption[]>(() => {
	const listed: SelectOption[] = jobs.value.map((j) => {
		const kind = jobKindLabel(j);
		return {
			value: codeLoopTarget(j.target, j.number),
			// A scheduled run's number is its fire time, so a schedule is
			// named by its title alone.
			label: j.scheduled ? j.title : `#${j.number} ${j.title}`.trim(),
			description: kind ? `${kind} · ${j.status}` : j.status,
			group: j.target,
			keywords: `${j.repo} ${j.status} ${j.state} ${kind}`,
		};
	});
	// A target saved from an earlier run of a schedule names the schedule.
	const earlier = pickedJob.value;
	if (earlier?.scheduled && earlier.number !== picked.value?.number)
		listed.push({
			value: jobValue.value,
			label: earlier.title,
			description: `schedule · ${earlier.status}`,
			group: earlier.target,
			keywords: "",
		});
	if (picked.value && missing.value)
		listed.push({
			value: jobValue.value,
			label: `#${picked.value.number} (no longer exists)`,
			description: "missing",
			group: picked.value.project,
			keywords: "",
		});
	return listed;
});
</script>
