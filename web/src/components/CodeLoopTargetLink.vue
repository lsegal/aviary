<template>
	<span v-if="picked" class="inline-flex items-center gap-1" data-testid="codeloop-target">
		<router-link :to="jobPath(owner, picked.project, picked.number)" data-testid="codeloop-target-link"
			class="text-blue-600 hover:underline dark:text-blue-400">{{ codeLoopTargetLabel(picked) }}</router-link>
		<span v-if="missing" role="alert" data-testid="codeloop-target-warning"
			class="inline-flex items-center gap-0.5 text-amber-700 dark:text-amber-400">
			<ExclamationTriangleIcon aria-hidden="true" class="h-3.5 w-3.5" /> job no longer exists
		</span>
	</span>
</template>

<script setup lang="ts">
// A Code Loop target as the task and workflow views show it: a link to the
// job's page, warning when the job no longer exists.
import ExclamationTriangleIcon from "@heroicons/vue/24/outline/ExclamationTriangleIcon";
import { computed, onMounted } from "vue";
import { useCodeLoopJobs } from "../composables/useCodeLoopJobs";
import { projectOwner } from "../lib/projectSettings";
import { jobPath } from "../lib/projects";
import { codeLoopTargetLabel, parseCodeLoopTarget } from "../lib/taskTargets";
import { useSettingsStore } from "../stores/settings";

const props = defineProps<{ target: string }>();

const settings = useSettingsStore();
const { loaded, load, find } = useCodeLoopJobs();
onMounted(() => void load());

const picked = computed(() => parseCodeLoopTarget(props.target));
const owner = computed(() =>
	picked.value ? projectOwner(settings.config, picked.value.project) : "",
);
const missing = computed(
	() =>
		!!picked.value &&
		loaded.value &&
		!find(picked.value.project, picked.value.number),
);
</script>
