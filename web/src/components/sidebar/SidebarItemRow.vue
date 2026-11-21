<template>
	<div class="group relative flex items-center rounded-lg transition-colors"
		:class="active ? 'bg-gray-100 dark:bg-gray-800' : 'hover:bg-gray-100 dark:hover:bg-gray-800'">
		<!-- One item under a sidebar section (a chat, task or file): its link, an
		     optional hover-only detail, and the "…" menu. -->
		<slot v-if="editing" name="editor" />
		<template v-else>
			<router-link :to="to" :title="label" data-testid="sidebar-item-link"
				class="min-w-0 flex-1 truncate py-2 pl-3 pr-1 text-sm"
				:class="active
					? 'font-semibold text-gray-950 dark:text-white'
					: 'text-gray-500 group-hover:text-gray-900 dark:text-gray-400 dark:group-hover:text-white'"
				@dblclick.prevent="emit('dblclick')">
				{{ label }}
			</router-link>
			<slot name="meta" />
			<SidebarItemMenu v-model:open="menuOpen" :label="label" :actions="actions" :testid="menuTestid"
				@select="(key) => emit('select', key)" />
		</template>
	</div>
</template>

<script setup lang="ts">
import type { RouteLocationRaw } from "vue-router";
import SidebarItemMenu from "./SidebarItemMenu.vue";
import type { SidebarItemAction } from "./types";

defineProps<{
	to: RouteLocationRaw;
	label: string;
	active: boolean;
	actions: SidebarItemAction[];
	menuTestid: string;
	/** Shows the editor slot (an inline rename) in place of the link. */
	editing?: boolean;
}>();
const emit = defineEmits<{ select: [key: string]; dblclick: [] }>();
const menuOpen = defineModel<boolean>("menuOpen", { default: false });
</script>
