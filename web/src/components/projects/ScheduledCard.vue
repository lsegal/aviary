<template>
	<article data-testid="scheduled-card"
		class="flex min-w-0 flex-col overflow-hidden rounded-xl border border-violet-200 bg-white dark:border-violet-900 dark:bg-gray-900">
		<div class="space-y-1.5 p-4">
			<div class="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs">
				<span class="rounded-full bg-violet-50 px-2 py-0.5 font-medium text-violet-700 dark:bg-violet-950/50 dark:text-violet-300">
					Scheduled
				</span>
				<span v-if="!schedule.enabled" class="rounded-full bg-gray-100 px-2 py-0.5 font-medium text-gray-600 dark:bg-gray-800 dark:text-gray-300">
					Paused
				</span>
				<span class="ml-auto truncate font-mono text-gray-500 dark:text-gray-400">{{ schedule.id }}</span>
			</div>
			<h3 class="text-base font-semibold leading-snug text-gray-900 dark:text-white">
				<router-link :to="to" class="hover:underline">{{ schedule.name }}</router-link>
			</h3>
			<p class="truncate text-xs text-gray-500 dark:text-gray-400" :title="schedule.prompt">{{ truncatePrompt(schedule.prompt, 120) }}</p>
		</div>
		<dl data-testid="scheduled-run-details"
			class="grid flex-1 grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1.5 border-t border-gray-100 px-4 py-3 text-xs dark:border-gray-800">
			<dt class="text-gray-500 dark:text-gray-400">Trigger</dt>
			<dd data-testid="scheduled-trigger" class="truncate text-gray-800 dark:text-gray-100">{{ triggerSummary(schedule) }}</dd>
			<dt class="text-gray-500 dark:text-gray-400">Next run</dt>
			<dd data-testid="scheduled-next" class="truncate text-gray-800 dark:text-gray-100">
				<template v-if="schedule.enabled && schedule.nextRun">
					{{ until(schedule.nextRun, now) }} <span class="text-gray-400">· {{ formatFire(schedule.nextRun) }}</span>
				</template>
				<template v-else-if="!schedule.enabled">paused</template>
				<template v-else>none</template>
			</dd>
			<dt class="text-gray-500 dark:text-gray-400">Last run</dt>
			<dd data-testid="scheduled-last" class="flex min-w-0 items-center gap-1.5 text-gray-800 dark:text-gray-100">
				<template v-if="last">
					<span class="h-1.5 w-1.5 shrink-0 rounded-full" :class="scheduleRunDot(last.status)" />
					<span class="truncate" :title="last.summary">{{ last.summary || last.status }}</span>
					<span class="shrink-0 text-gray-400">· {{ age(last.fireTime, now) }} ago</span>
				</template>
				<template v-else>never</template>
			</dd>
		</dl>
		<div class="flex flex-wrap items-center gap-2 border-t border-gray-100 px-3 py-2.5 dark:border-gray-800">
			<router-link :to="to" aria-label="Edit schedule" title="Edit this schedule"
				class="rounded-lg border border-gray-200 p-1.5 text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800">
				<ArrowsPointingOutIcon aria-hidden="true" class="h-4 w-4" />
			</router-link>
			<span v-if="schedule.history[0]?.status === 'running'" class="text-xs text-blue-600 dark:text-blue-400">running now</span>
		</div>
	</article>
</template>

<script setup lang="ts">
import ArrowsPointingOutIcon from "@heroicons/vue/24/outline/ArrowsPointingOutIcon";
import { computed } from "vue";
import type { RouteLocationRaw } from "vue-router";
import {
	age,
	formatFire,
	lastScheduleRun,
	type ProjectSchedule,
	scheduleRunDot,
	triggerSummary,
	truncatePrompt,
	until,
} from "../../lib/projects";

const props = defineProps<{
	schedule: ProjectSchedule;
	now: number;
	/** Where the box opens its detail view. */
	to: RouteLocationRaw;
}>();

const last = computed(() => lastScheduleRun(props.schedule));
</script>
