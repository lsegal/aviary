<template>
	<SidebarItemList :items="tasks" :item-key="(t) => t.name" more-testid="sidebar-tasks-more"
		empty="No tasks yet" data-testid="sidebar-tasks">
		<template #default="{ item: t }">
			<SidebarItemRow data-testid="task-row" :data-task="t.name" :data-paused="isPaused(t)"
				:to="taskSessionPath(agent, t.name)" :label="t.name" :active="t.name === activeName"
				:muted="isPaused(t)" :tooltip="isPaused(t) ? 'Paused' : undefined" :actions="actionsFor(t)"
				menu-testid="task-menu-button" @select="(key) => select(key, t)">
				<template #meta>
					<span v-if="isPaused(t)" data-testid="task-row-paused" title="Paused"
						class="mr-1 inline-flex shrink-0 text-amber-600 dark:text-amber-400">
						<PauseIcon aria-hidden="true" class="h-3.5 w-3.5" />
						<span class="sr-only">Paused</span>
					</span>
				</template>
			</SidebarItemRow>
		</template>
	</SidebarItemList>
</template>

<script setup lang="ts">
import PauseIcon from "@heroicons/vue/20/solid/PauseIcon";
import Cog6ToothIcon from "@heroicons/vue/24/outline/Cog6ToothIcon";
import DocumentDuplicateIcon from "@heroicons/vue/24/outline/DocumentDuplicateIcon";
import PauseCircleIcon from "@heroicons/vue/24/outline/PauseCircleIcon";
import PlayCircleIcon from "@heroicons/vue/24/outline/PlayCircleIcon";
import { computed } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useAgentItemDialogs } from "../../composables/useAgentItemDialogs";
import { useTaskPause } from "../../composables/useTaskPause";
import {
	type AgentTask,
	activeTaskName,
	agentTaskPath,
	isWorkflow,
	taskSessionPath,
	useSettingsStore,
} from "../../stores/settings";
import SidebarItemList from "./SidebarItemList.vue";
import SidebarItemRow from "./SidebarItemRow.vue";
import type { SidebarItemAction } from "./types";

const props = defineProps<{ agent: string }>();

const settings = useSettingsStore();
const route = useRoute();
const router = useRouter();
const { open } = useAgentItemDialogs();
const { setTaskEnabled, isPending } = useTaskPause();

const isPaused = (t: AgentTask) => t.enabled === false;

// Paused tasks sort after the active ones, each kept in config order.
const tasks = computed(() => {
	const all =
		settings.config?.agents
			.find((a) => a.name === props.agent)
			?.tasks.filter((t) => t.name && !isWorkflow(t)) ?? [];
	return [
		...all.filter((t) => !isPaused(t)),
		...all.filter((t) => isPaused(t)),
	];
});

// The task whose session or settings are showing.
const activeName = computed(() => activeTaskName(route, props.agent));

function actionsFor(t: AgentTask): SidebarItemAction[] {
	const paused = isPaused(t);
	return [
		{
			key: "toggle",
			label: paused ? "Resume" : "Pause",
			icon: paused ? PlayCircleIcon : PauseCircleIcon,
			disabled: isPending(props.agent, t.name),
		},
		{ key: "settings", label: "Settings", icon: Cog6ToothIcon },
		{ key: "duplicate", label: "Duplicate…", icon: DocumentDuplicateIcon },
		{ key: "delete", label: "Delete", danger: true },
	];
}

function select(key: string, t: AgentTask) {
	if (key === "toggle") void setTaskEnabled(props.agent, t.name, isPaused(t));
	else if (key === "settings")
		void router.push(agentTaskPath(props.agent, t.name));
	else if (key === "duplicate")
		open({
			kind: "task",
			agent: props.agent,
			mode: "create",
			duplicate: t.name,
		});
	else open({ kind: "task", agent: props.agent, mode: "delete", name: t.name });
}
</script>
