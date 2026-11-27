<template>
	<DropdownMenuRoot>
		<DropdownMenuTrigger asChild>
			<button ref="trigger" type="button" data-testid="board-more-menu" aria-label="More board actions"
				title="More board actions" :class="HEADER_ICON_CLS">
				<EllipsisHorizontalIcon aria-hidden="true" class="h-5 w-5" />
			</button>
		</DropdownMenuTrigger>
		<DropdownMenuPortal>
			<DropdownMenuContent align="end" :side-offset="6" :collision-padding="8" data-testid="board-more-items"
				class="z-50 max-h-[var(--radix-dropdown-menu-content-available-height)] min-w-60 overflow-y-auto rounded-xl border border-gray-200 bg-white p-1 shadow-lg dark:border-gray-700 dark:bg-gray-900">
				<DropdownMenuItem data-testid="board-refresh" :disabled="checking" :class="itemClass"
					@select="emit('refresh')">
					<ArrowPathIcon aria-hidden="true" :class="[iconClass, { 'animate-spin': checking }]" />
					<span>Refresh</span>
				</DropdownMenuItem>
				<DropdownMenuCheckboxItem v-if="logsInMenu" data-testid="board-logs" :checked="logsOpen"
					:class="itemClass" @update:checked="emit('update:logsOpen', $event)">
					<DocumentTextIcon aria-hidden="true" :class="iconClass" />
					<span class="flex-1">Logs</span>
					<DropdownMenuItemIndicator>
						<CheckIcon aria-hidden="true" class="h-4 w-4" />
					</DropdownMenuItemIndicator>
				</DropdownMenuCheckboxItem>
				<template v-if="modeTarget">
					<DropdownMenuSeparator :class="separatorClass" />
					<DropdownMenuLabel :class="labelClass">Issue discovery</DropdownMenuLabel>
					<DropdownMenuRadioGroup :model-value="mode.choice.value" @update:model-value="mode.pick">
						<DropdownMenuRadioItem v-for="option in PROJECT_MODES" :key="option.value" :value="option.value"
							:disabled="mode.saving.value" :data-testid="`board-mode-${option.value}`" :class="itemClass">
							<component :is="option.value === 'manual' ? EyeSlashIcon : EyeIcon" aria-hidden="true" :class="iconClass" />
							<span class="flex flex-1 flex-col">
								<span>{{ option.label }}</span>
								<span class="text-xs text-gray-500 dark:text-gray-400">{{ option.hint }}</span>
							</span>
							<DropdownMenuItemIndicator>
								<CheckIcon aria-hidden="true" class="h-4 w-4" />
							</DropdownMenuItemIndicator>
						</DropdownMenuRadioItem>
					</DropdownMenuRadioGroup>
					<p v-if="mode.error.value" data-testid="board-mode-error" class="px-3 py-1.5 text-xs text-red-600 dark:text-red-400">
						{{ mode.error.value }}
					</p>
				</template>
				<template v-if="targets.length">
					<DropdownMenuSeparator :class="separatorClass" />
					<DropdownMenuLabel :class="labelClass">{{ scopeLabel }}</DropdownMenuLabel>
					<DropdownMenuItem v-for="option in clearOptions" :key="option.which" :disabled="option.count === 0"
						:data-testid="`clear-jobs-${option.which}`" :class="itemClass" @select="pending = option">
						<TrashIcon aria-hidden="true" :class="iconClass" />
						<span class="flex-1">{{ option.label }}</span>
						<span class="tabular-nums text-xs text-gray-500 dark:text-gray-400">{{ option.count }}</span>
					</DropdownMenuItem>
				</template>
				<DropdownMenuSeparator :class="separatorClass" />
				<DropdownMenuItem as-child>
					<router-link :to="settingsPath" data-testid="project-settings-link" :class="itemClass">
						<Cog6ToothIcon aria-hidden="true" :class="iconClass" />
						<span>Settings</span>
					</router-link>
				</DropdownMenuItem>
			</DropdownMenuContent>
		</DropdownMenuPortal>
	</DropdownMenuRoot>
	<ConfirmDialog :open="pending !== null" :title="pending ? `${pending.label}?` : ''" :action="clear"
		confirm-label="Clear" busy-label="Clearing…" test-id="clear-jobs-confirm" @update:open="(v) => { if (!v) pending = null }">
		<p>
			This removes the {{ pending?.noun }} {{ scopeText }} from the board and
			Code Loop's job store, with their transcripts. Running, queued and paused jobs are never cleared.
			GitHub is not touched, and a later run of the same issue still shows.
		</p>
	</ConfirmDialog>
</template>

<script setup lang="ts">
// The Code Loop board header's "…" menu (#1488): Refresh, the clear actions
// (#1460) and Settings always live here, and Logs and the issue discovery
// (Watch) choice join them when the header is too narrow for their buttons.
// Each clear action asks through the shared confirm dialog first.

import CheckIcon from "@heroicons/vue/16/solid/CheckIcon";
import ArrowPathIcon from "@heroicons/vue/24/outline/ArrowPathIcon";
import Cog6ToothIcon from "@heroicons/vue/24/outline/Cog6ToothIcon";
import DocumentTextIcon from "@heroicons/vue/24/outline/DocumentTextIcon";
import EllipsisHorizontalIcon from "@heroicons/vue/24/outline/EllipsisHorizontalIcon";
import EyeIcon from "@heroicons/vue/24/outline/EyeIcon";
import EyeSlashIcon from "@heroicons/vue/24/outline/EyeSlashIcon";
import TrashIcon from "@heroicons/vue/24/outline/TrashIcon";
import {
	DropdownMenuCheckboxItem,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuItemIndicator,
	DropdownMenuLabel,
	DropdownMenuPortal,
	DropdownMenuRadioGroup,
	DropdownMenuRadioItem,
	DropdownMenuRoot,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "radix-vue";
import { computed, ref } from "vue";
import type { RouteLocationRaw } from "vue-router";
import { useMCP } from "../../composables/useMCP";
import { useProjectMode } from "../../composables/useProjectMode";
import { HEADER_ICON_CLS } from "../../lib/headerActions";
import {
	PROJECT_MODES,
	type ProjectMode,
	type ProjectModeChoice,
} from "../../lib/projects";
import ConfirmDialog from "../ui/ConfirmDialog.vue";

type ClearWhich = "done" | "failed" | "finished";
interface ClearOption {
	which: ClearWhich;
	label: string;
	noun: string;
	count: number;
}

const props = defineProps<{
	/** The board's projects; one names a single project's board. */
	targets: string[];
	/** How many done and failed jobs the board shows. */
	done: number;
	failed: number;
	checking: boolean;
	settingsPath: RouteLocationRaw;
	/** Logs is a menu item instead of a header button. */
	logsInMenu: boolean;
	logsOpen: boolean;
	/** The project whose issue discovery choice is in the menu, if any. */
	modeTarget: string | null;
	manual: boolean;
	defaultMode: ProjectMode;
}>();
const emit = defineEmits<{
	refresh: [];
	"update:logsOpen": [open: boolean];
	cleared: [count: number];
	modeChanged: [mode: ProjectModeChoice];
}>();

const { callTool } = useMCP();
const trigger = ref<HTMLButtonElement | null>(null);
defineExpose({ trigger });

const mode = useProjectMode(
	{
		get target() {
			return props.modeTarget ?? "";
		},
		get manual() {
			return props.manual;
		},
		get defaultMode() {
			return props.defaultMode;
		},
	},
	(m) => emit("modeChanged", m),
);

const itemClass =
	"flex cursor-pointer items-center gap-2 rounded-lg px-3 py-2 text-sm text-gray-700 outline-none data-[disabled]:cursor-not-allowed data-[highlighted]:bg-gray-100 data-[disabled]:opacity-50 dark:text-gray-200 dark:data-[highlighted]:bg-gray-800";
const iconClass = "h-4 w-4 shrink-0 text-gray-500 dark:text-gray-400";
const labelClass =
	"px-3 py-1.5 text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400";
const separatorClass = "my-1 h-px bg-gray-200 dark:bg-gray-700";

const pending = ref<ClearOption | null>(null);
const single = computed(() => props.targets.length === 1);
const scopeLabel = computed(() =>
	single.value ? "Clear this project's jobs" : "Clear across these projects",
);
const scopeText = computed(() =>
	single.value ? `of ${props.targets[0]}` : "across the board's projects",
);
const clearOptions = computed<ClearOption[]>(() => [
	{ which: "done", label: "Clear done", noun: "done jobs", count: props.done },
	{
		which: "failed",
		label: "Clear failed",
		noun: "failed jobs",
		count: props.failed,
	},
	{
		which: "finished",
		label: "Clear all finished",
		noun: "done and failed jobs",
		count: props.done + props.failed,
	},
]);

async function clear() {
	const option = pending.value;
	if (!option) return;
	await callTool("project_job_clear", {
		targets: props.targets,
		which: option.which,
	});
	pending.value = null;
	emit("cleared", option.count);
}
</script>
