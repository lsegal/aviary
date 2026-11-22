<template>
	<SidebarItemList :items="tasks" :item-key="(t) => t.name" more-testid="sidebar-tasks-more"
		empty="No tasks yet" data-testid="sidebar-tasks">
		<template #default="{ item: t }">
			<SidebarItemRow data-testid="task-row" :data-task="t.name" :to="taskSessionPath(agent, t.name)"
				:label="t.name" :active="t.name === activeName" :actions="actions" menu-testid="task-menu-button"
				@select="(key) => select(key, t.name)" />
		</template>
	</SidebarItemList>
</template>

<script setup lang="ts">
import Cog6ToothIcon from "@heroicons/vue/24/outline/Cog6ToothIcon";
import { computed } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useAgentItemDialogs } from "../../composables/useAgentItemDialogs";
import {
	activeTaskName,
	agentTaskPath,
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

const tasks = computed(
	() =>
		settings.config?.agents
			.find((a) => a.name === props.agent)
			?.tasks.filter((t) => t.name) ?? [],
);

// The task whose session or settings are showing.
const activeName = computed(() => activeTaskName(route, props.agent));

const actions: SidebarItemAction[] = [
	{ key: "settings", label: "Settings", icon: Cog6ToothIcon },
	{ key: "delete", label: "Delete", danger: true },
];

function select(key: string, name: string) {
	if (key === "settings") void router.push(agentTaskPath(props.agent, name));
	else open({ kind: "task", agent: props.agent, mode: "delete", name });
}
</script>
