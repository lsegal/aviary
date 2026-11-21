<template>
	<div class="flex shrink-0 items-center gap-0.5" role="group" :aria-label="`${row.task.name} actions`" @click.stop>
		<button v-for="a in actions" :key="a.key" type="button" :data-testid="`task-${a.key}`" :aria-label="a.label" :title="a.label"
			:disabled="busy && a.key !== 'edit' && a.key !== 'runs'"
			class="rounded-md p-1 text-gray-500 hover:bg-gray-100 hover:text-gray-900 disabled:opacity-40 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-white"
			@click="emit('action', a.key)">
			<component :is="a.icon" aria-hidden="true" class="h-4 w-4" />
		</button>
	</div>
</template>

<script setup lang="ts">
import ListBulletIcon from "@heroicons/vue/24/outline/ListBulletIcon";
import PauseIcon from "@heroicons/vue/24/outline/PauseIcon";
import PencilIcon from "@heroicons/vue/24/outline/PencilIcon";
import PlayCircleIcon from "@heroicons/vue/24/outline/PlayCircleIcon";
import PlayIcon from "@heroicons/vue/24/outline/PlayIcon";
import { computed } from "vue";
import type { TaskAction, TaskRow } from "./types";

// A task's quick actions: run now, pause or resume, edit, and view its runs.
const props = defineProps<{ row: TaskRow; busy: boolean }>();
const emit = defineEmits<{ action: [action: TaskAction] }>();

const actions = computed(
	(): { key: TaskAction; label: string; icon: unknown }[] => [
		{ key: "run", label: "Run now", icon: PlayIcon },
		props.row.enabled
			? { key: "toggle", label: "Pause", icon: PauseIcon }
			: { key: "toggle", label: "Resume", icon: PlayCircleIcon },
		{ key: "edit", label: "Edit", icon: PencilIcon },
		{ key: "runs", label: "View runs", icon: ListBulletIcon },
	],
);
</script>
