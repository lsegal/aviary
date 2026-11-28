<template>
	<DropdownMenuRoot>
		<DropdownMenuTrigger asChild>
			<button ref="trigger" type="button" data-testid="board-more-menu" aria-label="More board actions"
				title="More board actions" :class="['relative', HEADER_ICON_CLS]"
				:aria-describedby="cliAlert ? cliAlertID : undefined">
				<EllipsisHorizontalIcon aria-hidden="true" class="h-5 w-5" />
				<template v-if="cliAlert">
					<!-- The details moved here, so their warning dot does too (#1580). -->
					<span aria-hidden="true" data-testid="board-more-menu-alert" :class="ALERT_DOT_CLS" />
					<span :id="cliAlertID" class="sr-only">{{ cliAlert }}</span>
				</template>
			</button>
		</DropdownMenuTrigger>
		<DropdownMenuPortal>
			<DropdownMenuContent align="end" :side-offset="6" :collision-padding="8" data-testid="board-more-items"
				class="z-50 max-h-[var(--radix-dropdown-menu-content-available-height)] min-w-60 max-w-[calc(100vw-1rem)] overflow-y-auto rounded-xl border border-gray-200 bg-white p-1 shadow-lg dark:border-gray-700 dark:bg-gray-900">
				<template v-if="hidden.length">
					<!-- The header items that moved here on a narrow phone, each
						shown here or in the header, never both (#1486, #1576). -->
					<DropdownMenuItem v-if="has('board-add-project')" data-testid="board-more-add-project" :class="itemClass"
						@select="emit('addProject')">
						<PlusIcon aria-hidden="true" :class="iconClass" />
						<span>Add project</span>
					</DropdownMenuItem>
					<DropdownMenuItem v-if="has('schedule-new')" data-testid="board-more-schedule" :disabled="!canSchedule"
						:class="itemClass" @select="emit('schedule')">
						<CalendarDaysIcon aria-hidden="true" :class="iconClass" />
						<span>Schedule task</span>
					</DropdownMenuItem>
					<DropdownMenuCheckboxItem v-if="has('prompt-chat-toggle')" data-testid="board-more-chat" :checked="chatOpen"
						:class="itemClass" @update:checked="emit('update:chatOpen', $event)">
						<ChatBubbleLeftRightIcon aria-hidden="true" :class="iconClass" />
						<span class="flex-1">Prompt chat</span>
						<DropdownMenuItemIndicator>
							<CheckIcon aria-hidden="true" class="h-4 w-4" />
						</DropdownMenuItemIndicator>
					</DropdownMenuCheckboxItem>
					<DropdownMenuSeparator v-if="has('queue-menu') && hasOtherAction" :class="separatorClass" />
					<DropdownMenuGroup v-if="has('queue-menu')" data-testid="board-more-queue" aria-label="Queue">
						<DropdownMenuLabel :class="labelClass">Queue</DropdownMenuLabel>
						<DropdownMenuItem v-for="option in queueChoices" :key="option.skill" :disabled="option.disabled"
							:data-testid="`board-more-queue-${option.skill}`" :title="option.title" :class="itemClass"
							@select="emit('queue', option.skill)">
							<component :is="option.icon" aria-hidden="true" :class="iconClass" />
							<span>{{ option.label }}</span>
						</DropdownMenuItem>
					</DropdownMenuGroup>
					<DropdownMenuSeparator v-if="hasAction" :class="separatorClass" />
				</template>
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
				<template v-if="status && has('board-info')">
					<DropdownMenuSeparator :class="separatorClass" />
					<DropdownMenuGroup data-testid="board-more-info" aria-label="Board details">
						<DropdownMenuLabel :class="labelClass">Board details</DropdownMenuLabel>
						<!-- The filter editor takes its own keys, so the menu's
							typeahead and Tab trap leave it alone; Esc cancels an
							edit before it closes the menu. -->
						<div class="px-3 py-1.5" @keydown="onDetailsKeydown">
							<BoardDetailsList v-model:editing="editingFilter" :status="status" :manual="manual"
								:filter-target="filterTarget" :now="now" @saved="emit('saved')" />
							<div v-if="status.clis.length" class="mt-3 border-t border-gray-200 pt-3 dark:border-gray-700 sm:hidden">
								<CLIProviderList stacked testid="board-more-clis" :clis="status.clis" />
							</div>
						</div>
					</DropdownMenuGroup>
				</template>
				<template v-if="status?.instanceID && has('instance-tag')">
					<DropdownMenuSeparator :class="separatorClass" />
					<DropdownMenuGroup data-testid="board-more-instance" aria-label="Instance">
						<DropdownMenuLabel :class="labelClass">Instance</DropdownMenuLabel>
						<p class="px-3 pb-1 text-xs text-gray-500 dark:text-gray-400">
							<code data-testid="board-more-instance-id" class="font-mono text-sm font-bold tracking-wider text-gray-900 dark:text-white">{{ status.instanceID }}</code>
							· signs its GitHub claims as <code class="font-mono">/aviary:{{ status.instanceID }}</code>
						</p>
						<DropdownMenuItem data-testid="board-more-instance-copy" :class="itemClass"
							@select.prevent="copy(status.instanceID)">
							<ClipboardDocumentIcon aria-hidden="true" :class="iconClass" />
							<span class="flex-1">Copy instance ID</span>
						</DropdownMenuItem>
						<p data-testid="board-more-instance-copy-status" role="status" aria-live="polite"
							class="px-3 text-xs text-gray-500 empty:hidden dark:text-gray-400">{{ copyStatus }}</p>
					</DropdownMenuGroup>
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
// Each clear action asks through the shared confirm dialog first. On a
// narrow phone the header's other items move in too, one at a time, until
// its single line fits (#1576): Chat, Schedule, Queue and Add project as
// items, then the board details and instance popovers as sections.

import CheckIcon from "@heroicons/vue/16/solid/CheckIcon";
import ArrowPathIcon from "@heroicons/vue/24/outline/ArrowPathIcon";
import CalendarDaysIcon from "@heroicons/vue/24/outline/CalendarDaysIcon";
import ChatBubbleLeftRightIcon from "@heroicons/vue/24/outline/ChatBubbleLeftRightIcon";
import ClipboardDocumentIcon from "@heroicons/vue/24/outline/ClipboardDocumentIcon";
import Cog6ToothIcon from "@heroicons/vue/24/outline/Cog6ToothIcon";
import DocumentTextIcon from "@heroicons/vue/24/outline/DocumentTextIcon";
import EllipsisHorizontalIcon from "@heroicons/vue/24/outline/EllipsisHorizontalIcon";
import EyeIcon from "@heroicons/vue/24/outline/EyeIcon";
import EyeSlashIcon from "@heroicons/vue/24/outline/EyeSlashIcon";
import PlusIcon from "@heroicons/vue/24/outline/PlusIcon";
import TrashIcon from "@heroicons/vue/24/outline/TrashIcon";
import {
	DropdownMenuCheckboxItem,
	DropdownMenuContent,
	DropdownMenuGroup,
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
import { computed, ref, useId } from "vue";
import type { RouteLocationRaw } from "vue-router";
import { useCopyText } from "../../composables/useCopyText";
import { useMCP } from "../../composables/useMCP";
import { useProjectMode } from "../../composables/useProjectMode";
import { ALERT_DOT_CLS, HEADER_ICON_CLS } from "../../lib/headerActions";
import {
	cliProblemSummary,
	PROJECT_MODES,
	type ProjectMode,
	type ProjectModeChoice,
	type ProjectStatus,
	type QueueSkill,
} from "../../lib/projects";
import ConfirmDialog from "../ui/ConfirmDialog.vue";
import BoardDetailsList from "./BoardDetailsList.vue";
import CLIProviderList from "./CLIProviderList.vue";
import { queueOptions } from "./queueOptions";

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
	/**
	 * The data-testids of the header items moved into this menu (#1576):
	 * board-add-project, queue-menu, schedule-new, prompt-chat-toggle,
	 * board-info and instance-tag.
	 */
	hidden?: readonly string[];
	/** The board's status, for its details and instance sections. */
	status?: ProjectStatus | null;
	/** The project whose filter the details edit; empty on aggregate boards. */
	filterTarget?: string;
	now?: number;
	chatOpen?: boolean;
	canSchedule?: boolean;
	/** A phone's header: "…" warns about the agent models' problems (#1580). */
	showClis?: boolean;
}>();
const emit = defineEmits<{
	refresh: [];
	"update:logsOpen": [open: boolean];
	cleared: [count: number];
	modeChanged: [mode: ProjectModeChoice];
	addProject: [];
	queue: [skill: QueueSkill];
	schedule: [];
	"update:chatOpen": [open: boolean];
	/** The issue filter was saved or reset from the details section. */
	saved: [];
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

const hidden = computed(() => props.hidden ?? []);
const has = (id: string) => hidden.value.includes(id);
const ACTIONS = [
	"board-add-project",
	"queue-menu",
	"schedule-new",
	"prompt-chat-toggle",
];
const hasAction = computed(() => ACTIONS.some(has));
// Queue's section follows the other actions behind a separator of its own.
const hasOtherAction = computed(() =>
	ACTIONS.some((id) => id !== "queue-menu" && has(id)),
);
const queueChoices = computed(() => queueOptions(props.status?.noMerge));
const filterTarget = computed(() => props.filterTarget ?? "");
const now = computed(() => props.now ?? Date.now());

const cliAlertID = useId();
const cliAlert = computed(() =>
	props.showClis && has("board-info")
		? cliProblemSummary(props.status?.clis ?? [])
		: "",
);

const editingFilter = ref(false);
function onDetailsKeydown(e: KeyboardEvent) {
	if (e.key !== "Escape" || editingFilter.value) e.stopPropagation();
}
const { status: copyStatus, copy } = useCopyText(
	"Couldn't copy; select the ID instead",
);

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
