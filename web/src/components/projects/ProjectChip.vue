<template>
	<!-- A card's project on a board of several (#1647): a chip that opens the
		project's board. A compact chip, or any chip at phone widths, shows the
		short repository name; the tooltip always names the full target. -->
	<router-link :to="to" :title="`${project} · open its Code Loop board`" :aria-label="`Open ${project}'s board`"
		class="min-w-0 max-w-full truncate rounded-full bg-gray-100 px-2 py-0.5 font-mono text-gray-700 hover:bg-gray-200 dark:bg-gray-800 dark:text-gray-200 dark:hover:bg-gray-700">
		<template v-if="compact || short === project">{{ short }}</template>
		<template v-else><span class="sm:hidden">{{ short }}</span><span class="hidden sm:inline">{{ project }}</span></template>
	</router-link>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { projectOwner } from "../../lib/projectSettings";
import { projectPath, shortProjectName } from "../../lib/projects";
import { useSettingsStore } from "../../stores/settings";

const props = defineProps<{
	/** The card's watch target, such as OWNER/REPO. */
	project: string;
	/** Always shows the short name, as a narrow card does. */
	compact?: boolean;
}>();

const settings = useSettingsStore();
const to = computed(() =>
	projectPath(projectOwner(settings.config, props.project), props.project),
);
const short = computed(() => shortProjectName(props.project));
</script>
