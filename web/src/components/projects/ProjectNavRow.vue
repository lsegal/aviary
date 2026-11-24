<template>
	<!-- One pill per project the board covers, so its page and its scheduled
		tasks stay reachable when the sidebar is collapsed and shows no subnav.
		The row scrolls sideways instead of wrapping, like the stat chips. -->
	<nav ref="row" aria-label="Projects" data-testid="project-nav"
		class="flex min-w-0 max-w-full flex-[1_1_16rem] items-center gap-1.5 overflow-x-auto whitespace-nowrap pb-1.5 text-xs [scrollbar-width:thin]"
		:style="fadeStyle" @scroll.passive="measure">
		<span v-for="p in rows" :key="p.name" data-testid="project-nav-item" :data-project="p.name"
			class="inline-flex shrink-0 items-center rounded-full border border-gray-200 dark:border-gray-700">
			<router-link :to="{ path: projectPath(agent, p.name), query: carried }" data-testid="project-nav-link"
				class="inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 hover:bg-gray-50 dark:hover:bg-gray-800"
				:title="`Open ${p.name}: ${p.label}`">
				<span class="h-1.5 w-1.5 rounded-full" :class="p.dot" data-testid="project-nav-dot" :data-state="p.state" />
				<b class="text-gray-800 dark:text-gray-100">{{ p.name }}</b>
				<span class="tabular-nums text-gray-500 dark:text-gray-400" data-testid="project-nav-count">· {{ p.count }}</span>
			</router-link>
			<router-link v-if="p.scheduled" :to="{ path: projectPath(agent, p.name), query: { state: 'scheduled' } }"
				data-testid="project-nav-scheduled"
				class="inline-flex items-center gap-1 rounded-full border-l border-gray-200 px-2.5 py-1 text-violet-700 hover:bg-violet-50 dark:border-gray-700 dark:text-violet-300 dark:hover:bg-violet-950/40"
				:title="`${p.name}'s scheduled tasks`">
				<CalendarDaysIcon aria-hidden="true" class="h-3.5 w-3.5" />
				Scheduled <span class="tabular-nums">{{ p.scheduled }}</span>
			</router-link>
		</span>
	</nav>
</template>

<script setup lang="ts">
import CalendarDaysIcon from "@heroicons/vue/24/outline/CalendarDaysIcon";
import { computed, nextTick, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { useProjectList } from "../../composables/useProjects";
import { useScrollFade } from "../../composables/useScrollFade";
import { projectPath } from "../../lib/projects";

const props = defineProps<{ agent: string; targets: string[] }>();

const route = useRoute();
// The live counts, with each project's scheduled tasks, polled like the
// board's own.
const { projects } = useProjectList(5_000, true);

// A project page opens with the board's job filter, which it shares.
const carried = computed(() =>
	typeof route.query.state === "string" && route.query.state
		? { state: route.query.state }
		: {},
);

const rows = computed(() =>
	props.targets.map((name) => {
		const p = projects.value.find((s) => s.name === name);
		const running = p?.running ?? 0;
		const waiting = p?.waiting ?? 0;
		const state = running ? "running" : waiting ? "waiting" : "idle";
		return {
			name,
			state,
			dot:
				state === "running"
					? "bg-blue-500"
					: state === "waiting"
						? "bg-amber-500"
						: "bg-gray-400",
			count: running
				? `${running} running`
				: waiting
					? `${waiting} waiting`
					: "0",
			label: running
				? `${running} running ${running === 1 ? "job" : "jobs"}`
				: waiting
					? `${waiting} waiting ${waiting === 1 ? "job" : "jobs"}`
					: "no active jobs",
			scheduled: p?.scheduled ?? 0,
		};
	}),
);

const row = ref<HTMLElement>();
const { fadeStyle, measure, observe } = useScrollFade(row);
watch(
	rows,
	async () => {
		await nextTick();
		observe();
	},
	{ deep: true },
);
</script>
