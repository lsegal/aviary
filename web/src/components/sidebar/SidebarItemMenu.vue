<template>
	<DropdownMenuRoot :open="open" @update:open="(v) => (open = v)">
		<!-- Collapsed while hidden, so the row's status icons sit flush right
		     instead of leaving a gap where the button would be. -->
		<DropdownMenuTrigger asChild>
			<button type="button" :aria-label="`More options for ${label}`" :data-testid="testid"
				class="inline-flex h-6 shrink-0 items-center justify-center overflow-hidden rounded-md text-gray-500 transition hover:bg-gray-200 hover:text-gray-900 focus:mr-1 focus:w-6 focus:opacity-100 group-hover:mr-1 group-hover:w-6 group-hover:opacity-100 group-focus-within:mr-1 group-focus-within:w-6 group-focus-within:opacity-100 [@media(hover:none)]:mr-1 [@media(hover:none)]:w-6 [@media(hover:none)]:opacity-100 dark:text-gray-400 dark:hover:bg-gray-700 dark:hover:text-white"
				:class="open ? 'mr-1 w-6 opacity-100' : 'w-0 opacity-0'">
				<svg viewBox="0 0 16 16" fill="currentColor" aria-hidden="true" class="h-4 w-4">
					<circle cx="3" cy="8" r="1.4" /><circle cx="8" cy="8" r="1.4" /><circle cx="13" cy="8" r="1.4" />
				</svg>
			</button>
		</DropdownMenuTrigger>
		<DropdownMenuContent align="start" :side-offset="4"
			class="z-50 min-w-36 rounded-xl border border-gray-200 bg-white p-1 shadow-lg dark:border-gray-700 dark:bg-gray-900">
			<DropdownMenuItem v-for="action in actions" :key="action.key" :disabled="action.disabled"
				:title="action.title"
				class="flex cursor-pointer items-center gap-2 rounded-lg px-3 py-2 text-sm outline-none data-[disabled]:cursor-default data-[disabled]:opacity-40"
				:class="action.danger
					? 'text-red-600 data-[highlighted]:bg-red-50 dark:text-red-400 dark:data-[highlighted]:bg-red-950/40'
					: 'text-gray-700 data-[highlighted]:bg-gray-100 dark:text-gray-200 dark:data-[highlighted]:bg-gray-800'"
				@select="emit('select', action.key)">
				<component :is="actionIcon(action)" aria-hidden="true" class="h-4 w-4" />
				{{ action.label }}
			</DropdownMenuItem>
		</DropdownMenuContent>
	</DropdownMenuRoot>
</template>

<script setup lang="ts">
import BackspaceIcon from "@heroicons/vue/24/outline/BackspaceIcon";
import PencilIcon from "@heroicons/vue/24/outline/PencilIcon";
import TrashIcon from "@heroicons/vue/24/outline/TrashIcon";
import {
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuRoot,
	DropdownMenuTrigger,
} from "radix-vue";
import type { SidebarItemAction } from "./types";

defineProps<{
	/** The item's name, for the button's accessible label. */
	label: string;
	actions: SidebarItemAction[];
	testid: string;
}>();
const emit = defineEmits<{ select: [key: string] }>();
const open = defineModel<boolean>("open", { default: false });

// Clear empties an item rather than deleting it.
function actionIcon(action: SidebarItemAction) {
	if (action.icon) return action.icon;
	if (action.key === "clear") return BackspaceIcon;
	return action.danger ? TrashIcon : PencilIcon;
}
</script>
