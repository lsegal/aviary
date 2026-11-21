<template>
	<div role="group" aria-label="Output view" data-testid="output-mode"
		class="flex shrink-0 overflow-hidden rounded-lg border border-gray-200 dark:border-gray-700">
		<button v-for="option in OPTIONS" :key="option.mode" type="button" :aria-label="option.label"
			:title="option.title" :aria-pressed="mode === option.mode" class="p-1.5"
			:class="mode === option.mode
				? 'bg-gray-100 text-gray-900 dark:bg-gray-700 dark:text-white'
				: 'text-gray-500 hover:bg-gray-50 dark:text-gray-400 dark:hover:bg-gray-800'"
			@click="mode = option.mode">
			<component :is="option.icon" aria-hidden="true" class="h-4 w-4" />
		</button>
	</div>
</template>

<script setup lang="ts">
import CommandLineIcon from "@heroicons/vue/24/outline/CommandLineIcon";
import ListBulletIcon from "@heroicons/vue/24/outline/ListBulletIcon";
import { useJobOutputMode } from "../../composables/useJobOutput";

const OPTIONS = [
	{
		mode: "rich",
		label: "Rich output",
		title: "Show messages and tool calls (all jobs)",
		icon: ListBulletIcon,
	},
	{
		mode: "raw",
		label: "Raw output",
		title: "Show the raw terminal scrollback (all jobs)",
		icon: CommandLineIcon,
	},
] as const;

const mode = useJobOutputMode();
</script>
