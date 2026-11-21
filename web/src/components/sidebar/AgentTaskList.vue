<template>
	<SidebarItemList :items="tasks" :item-key="(t) => t.name" more-testid="sidebar-tasks-more"
		empty="No tasks yet" data-testid="sidebar-tasks">
		<template #default="{ item: t }">
			<SidebarItemRow data-testid="task-row" :data-task="t.name" :to="agentTaskPath(agent, t.name)"
				:label="t.name" :active="t.name === activeName" :actions="actions" menu-testid="task-menu-button"
				@select="(key) => open({ kind: 'task', agent, mode: key === 'delete' ? 'delete' : 'edit', name: t.name })" />
		</template>
	</SidebarItemList>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";
import { useAgentItemDialogs } from "../../composables/useAgentItemDialogs";
import { agentTaskPath, useSettingsStore } from "../../stores/settings";
import SidebarItemList from "./SidebarItemList.vue";
import SidebarItemRow from "./SidebarItemRow.vue";
import { editDeleteActions } from "./types";

const props = defineProps<{ agent: string }>();

const settings = useSettingsStore();
const route = useRoute();
const { open } = useAgentItemDialogs();

const tasks = computed(
	() =>
		settings.config?.agents
			.find((a) => a.name === props.agent)
			?.tasks.filter((t) => t.name) ?? [],
);

// The Tasks page opens on its ?task, or on the first task.
const activeName = computed(() => {
	if (route.path !== agentTaskPath(props.agent).path) return "";
	const q = route.query.task;
	return typeof q === "string" && q ? q : (tasks.value[0]?.name ?? "");
});

const actions = editDeleteActions("Edit");
</script>
