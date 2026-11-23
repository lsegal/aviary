<template>
	<div>
		<AppCombobox :model-value="value" :options="options" :aria-label="ariaLabel ?? 'Code Loop job'"
			data-testid="codeloop-job-select" :invalid="!!warning"
			:placeholder="loading ? 'Loading Code Loop jobs…' : 'Choose a Code Loop job…'" empty-text="No matching Code Loop jobs"
			@update:model-value="emit('update:modelValue', String($event))" />
		<p v-if="warning" role="alert" data-testid="codeloop-job-warning"
			class="mt-1 text-xs text-amber-700 dark:text-amber-400">{{ warning }}</p>
	</div>
</template>

<script setup lang="ts">
// A Code Loop job picked as a task's or workflow's output target, shared by
// the task and workflow forms: lists every issue job, grouped by project and
// searchable by number, title and status, and stores its
// "codeloop:<project>#<number>" target.
import { computed, onMounted } from "vue";
import { useCodeLoopJobs } from "../composables/useCodeLoopJobs";
import {
	CODELOOP_TARGET_PREFIX,
	codeLoopTarget,
	parseCodeLoopTarget,
} from "../lib/taskTargets";
import { AppCombobox, type SelectOption } from "./ui/select";

const props = defineProps<{ modelValue: string; ariaLabel?: string }>();
const emit = defineEmits<{ "update:modelValue": [value: string] }>();

const { jobs, loaded, loading, load, find } = useCodeLoopJobs();
onMounted(() => void load(true));

// A bare prefix is the target a form starts from before a job is picked.
const value = computed(() =>
	props.modelValue.trim() === CODELOOP_TARGET_PREFIX ? "" : props.modelValue,
);
const picked = computed(() => parseCodeLoopTarget(value.value));
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
	const listed: SelectOption[] = jobs.value.map((j) => ({
		value: codeLoopTarget(j.target, j.number),
		label: `#${j.number} ${j.title}`.trim(),
		description: j.status,
		group: j.target,
		keywords: `${j.repo} ${j.status} ${j.state}`,
	}));
	if (picked.value && missing.value)
		listed.push({
			value: value.value,
			label: `#${picked.value.number} (no longer exists)`,
			description: "missing",
			group: picked.value.project,
			keywords: "",
		});
	return listed;
});
</script>
