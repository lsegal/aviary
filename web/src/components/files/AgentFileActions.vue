<template>
	<!-- The Files page's actions in the shared page header (#1147): Refresh,
		Sync Templates and Save, then the preview toggle and a "..." menu that
		renames or deletes the open file through the sidebar's dialogs. -->
	<div data-testid="agent-file-bar" class="flex min-w-0 max-w-full flex-nowrap items-center gap-2">
		<transition name="save-indicator">
			<div v-if="state.saveFlash" class="flex items-center gap-1.5 text-emerald-600 dark:text-emerald-400">
				<CheckCircleIcon aria-hidden="true" class="h-5 w-5" />
				<span class="text-xs font-medium">Saved</span>
			</div>
		</transition>
		<!-- On phones Refresh and Sync Templates show only their icons, so the
			actions fit one row (#1347). -->
		<button type="button" :class="HEADER_COLLAPSE_CLS" :disabled="state.loading" :aria-label="refreshLabel"
			:title="refreshLabel" @click="settings.refreshAgentFiles(agent)">
			<ArrowPathIcon aria-hidden="true" :class="[HEADER_COLLAPSE_ICON_CLS, { 'animate-spin': state.loading }]" />
			<span :class="HEADER_COLLAPSE_LABEL_CLS">{{ refreshLabel }}</span>
		</button>
		<button type="button" :class="HEADER_COLLAPSE_CLS" :disabled="state.syncing" :aria-label="syncLabel"
			:title="syncLabel" @click="settings.syncAgentTemplates(agent)">
			<DocumentDuplicateIcon aria-hidden="true" :class="HEADER_COLLAPSE_ICON_CLS" />
			<span :class="HEADER_COLLAPSE_LABEL_CLS">{{ syncLabel }}</span>
		</button>
		<button type="button" data-testid="agent-file-save" :class="HEADER_ACTION_CLS"
			:disabled="!agent || !state.selectedFile || state.saving" @click="settings.saveAgentFile(agent)">
			<ArrowDownTrayIcon aria-hidden="true" class="h-4 w-4" />
			{{ state.saving ? "Saving…" : "Save" }}
		</button>
		<FilePreviewToggle :file-name="state.selectedFile" />
		<DropdownMenuRoot>
			<DropdownMenuTrigger asChild>
				<button type="button" data-testid="agent-file-menu" aria-label="More file actions" title="More file actions"
					:class="HEADER_ICON_CLS" :disabled="!agent">
					<EllipsisHorizontalIcon aria-hidden="true" class="h-5 w-5" />
				</button>
			</DropdownMenuTrigger>
			<DropdownMenuContent align="end" :side-offset="4"
				class="z-50 min-w-40 rounded-xl border border-gray-200 bg-white p-1 shadow-lg dark:border-gray-700 dark:bg-gray-900">
				<DropdownMenuItem v-for="action in menuActions" :key="action.key" :disabled="locked" :title="lockedTitle"
					class="flex cursor-pointer items-center gap-2 rounded-lg px-3 py-2 text-sm outline-none data-[disabled]:cursor-default data-[disabled]:opacity-40"
					:class="action.danger
						? 'text-red-600 data-[highlighted]:bg-red-50 dark:text-red-400 dark:data-[highlighted]:bg-red-950/40'
						: 'text-gray-700 data-[highlighted]:bg-gray-100 dark:text-gray-200 dark:data-[highlighted]:bg-gray-800'"
					@select="openItemDialog({ kind: 'file', agent, mode: action.key === 'delete' ? 'delete' : 'edit', name: state.selectedFile })">
					<component :is="action.icon" aria-hidden="true" class="h-4 w-4" />
					{{ action.label }}
				</DropdownMenuItem>
			</DropdownMenuContent>
		</DropdownMenuRoot>
	</div>
</template>

<script setup lang="ts">
import ArrowDownTrayIcon from "@heroicons/vue/24/outline/ArrowDownTrayIcon";
import ArrowPathIcon from "@heroicons/vue/24/outline/ArrowPathIcon";
import CheckCircleIcon from "@heroicons/vue/24/outline/CheckCircleIcon";
import DocumentDuplicateIcon from "@heroicons/vue/24/outline/DocumentDuplicateIcon";
import EllipsisHorizontalIcon from "@heroicons/vue/24/outline/EllipsisHorizontalIcon";
import PencilIcon from "@heroicons/vue/24/outline/PencilIcon";
import TrashIcon from "@heroicons/vue/24/outline/TrashIcon";
import {
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuRoot,
	DropdownMenuTrigger,
} from "radix-vue";
import { computed, inject } from "vue";
import { useAgentItemDialogs } from "../../composables/useAgentItemDialogs";
import {
	HEADER_ACTION_CLS,
	HEADER_ICON_CLS,
	HEADER_COLLAPSE_CLS,
	HEADER_COLLAPSE_ICON_CLS,
	HEADER_COLLAPSE_LABEL_CLS,
} from "../../lib/headerActions";
import { settingsViewContextKey } from "../settings/context";
import FilePreviewToggle from "./FilePreviewToggle.vue";

const props = defineProps<{ agent: string }>();

const settings = inject(settingsViewContextKey);
const { open: openItemDialog } = useAgentItemDialogs();

const state = computed(() => settings.getAgentFileState(props.agent));
const refreshLabel = computed(() =>
	state.value.loading ? "Loading…" : "Refresh",
);
const syncLabel = computed(() =>
	state.value.syncing ? "Syncing…" : "Sync Templates",
);
const locked = computed(
	() =>
		!state.value.selectedFile ||
		settings.isProtectedAgentFile(state.value.selectedFile),
);
const lockedTitle = computed(() =>
	!state.value.selectedFile
		? "Open a file first"
		: locked.value
			? "Built-in files cannot be renamed or deleted"
			: undefined,
);

const menuActions = [
	{ key: "rename", label: "Rename", icon: PencilIcon, danger: false },
	{ key: "delete", label: "Delete", icon: TrashIcon, danger: true },
];
</script>
<style scoped>
.save-indicator-enter-active,
.save-indicator-leave-active {
  transition:
    opacity 180ms ease,
    transform 180ms ease;
}

.save-indicator-enter-from,
.save-indicator-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}
</style>
