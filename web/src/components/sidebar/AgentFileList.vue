<template>
	<SidebarItemList :items="files" :item-key="(f) => f" more-testid="sidebar-files-more"
		empty="No files yet" data-testid="sidebar-files">
		<template #default="{ item: f }">
			<SidebarItemRow data-testid="file-row" :data-file="f" :to="agentFilePath(agent, f)" :label="f"
				:active="f === activeName" :actions="actionsFor(f)" menu-testid="file-menu-button"
				@select="(key) => open({ kind: 'file', agent, mode: key === 'delete' ? 'delete' : 'edit', name: f })" />
		</template>
	</SidebarItemList>
</template>

<script setup lang="ts">
import { computed, onMounted, watch } from "vue";
import { useRoute } from "vue-router";
import { useAgentItemDialogs } from "../../composables/useAgentItemDialogs";
import {
	agentFilePath,
	isProtectedAgentFile,
	useAgentFilesStore,
} from "../../stores/agentFiles";
import SidebarItemList from "./SidebarItemList.vue";
import SidebarItemRow from "./SidebarItemRow.vue";
import { editDeleteActions } from "./types";

const props = defineProps<{ agent: string }>();

const store = useAgentFilesStore();
const route = useRoute();
const { open } = useAgentItemDialogs();

const files = computed(() => store.files(props.agent));

// The Files page opens on its ?file, or on the first file.
const activeName = computed(() => {
	if (route.path !== agentFilePath(props.agent).path) return "";
	const q = route.query.file;
	return typeof q === "string" && q ? q : (files.value[0] ?? "");
});

const editable = editDeleteActions("Edit");
const builtIn = editDeleteActions("Edit", {
	locked: true,
	lockedTitle: "Built-in files cannot be renamed or deleted",
});
function actionsFor(file: string) {
	return isProtectedAgentFile(file) ? builtIn : editable;
}

function reload() {
	store
		.load(props.agent)
		.catch((e) => console.error("Failed to load agent files", e));
}
watch(() => props.agent, reload);
onMounted(reload);
</script>
