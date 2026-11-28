<template>
	<HeaderInfoPopover v-if="status.instanceID && !hideInstance" testid="instance-tag" label="Instance ID">
		<template #icon><TagIcon aria-hidden="true" class="h-4 w-4" /></template>
		<div class="flex items-center gap-2">
			<span class="text-xs font-medium text-gray-500 dark:text-gray-400">Instance</span>
			<code data-testid="instance-id" class="font-mono text-sm font-bold tracking-wider text-gray-900 dark:text-white">{{ status.instanceID }}</code>
			<button type="button" data-testid="instance-id-copy" :aria-label="`Copy instance ID ${status.instanceID}`"
				title="Copy instance ID"
				class="inline-flex items-center gap-1 rounded border border-gray-200 px-1.5 py-0.5 text-xs text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800"
				@click="copyID">
				<ClipboardDocumentIcon aria-hidden="true" class="h-3.5 w-3.5" />Copy
			</button>
		</div>
		<p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
			This instance signs its GitHub claims as <code class="font-mono">/aviary:{{ status.instanceID }}</code>.
		</p>
		<p data-testid="instance-id-copy-status" role="status" aria-live="polite"
			class="text-xs text-gray-500 empty:hidden dark:text-gray-400">{{ copyStatus }}</p>
	</HeaderInfoPopover>
	<HeaderInfoPopover v-if="!hideInfo" testid="board-info" label="Board details" :alert="cliAlert"
		@escape-key-down="onEscape">
		<template #icon><InformationCircleIcon aria-hidden="true" class="h-4 w-4" /></template>
		<BoardDetailsList v-model:editing="editing" :status="status" :manual="manual" :filter-target="filterTarget"
			:now="now" @saved="emit('saved')" />
		<div v-if="status.clis.length" class="mt-3 border-t border-gray-200 pt-3 dark:border-gray-700 sm:hidden">
			<CLIProviderList stacked testid="board-info-clis" :clis="status.clis" />
		</div>
	</HeaderInfoPopover>
</template>

<script setup lang="ts">
// The Code Loop board header's icons beside its status chips (#1570): a tag
// opening the instance ID with a copy button, and an info icon opening what
// the board watches (the issue filter and its editor from #1213, the mode
// from #1375, delivery, the last check and the poll interval), in place of
// the instance badge and subtitle that took up a phone's header. On a narrow
// phone either icon can move into the header's "…" menu (#1576). Below sm
// the details also list the agent models and quota, hidden by the same CSS
// breakpoint that shows the board's inline row (#1582), and on a phone the
// icon carries a warning dot while one of them is signed out, out of quota
// or failing (#1580).
import ClipboardDocumentIcon from "@heroicons/vue/24/outline/ClipboardDocumentIcon";
import InformationCircleIcon from "@heroicons/vue/24/outline/InformationCircleIcon";
import TagIcon from "@heroicons/vue/24/outline/TagIcon";
import { computed, ref } from "vue";
import { useCopyText } from "../../composables/useCopyText";
import { cliProblemSummary, type ProjectStatus } from "../../lib/projects";
import HeaderInfoPopover from "../ui/HeaderInfoPopover.vue";
import BoardDetailsList from "./BoardDetailsList.vue";
import CLIProviderList from "./CLIProviderList.vue";

const props = defineProps<{
	status: ProjectStatus;
	manual: boolean;
	/** The project whose filter the editor changes; empty on aggregate boards. */
	filterTarget: string;
	now: number;
	/** The instance tag is in the header's "…" menu instead (#1576). */
	hideInstance?: boolean;
	/** The details icon is in the header's "…" menu instead (#1576). */
	hideInfo?: boolean;
	/** A phone's header: the icon warns about the agent models' problems (#1580). */
	showClis?: boolean;
}>();
const emit = defineEmits<{
	/** The issue filter was saved or reset. */
	saved: [];
}>();

const editing = ref(false);
const cliAlert = computed(() =>
	props.showClis ? cliProblemSummary(props.status.clis ?? []) : "",
);
const { status: copyStatus, copy } = useCopyText(
	"Couldn't copy; select the ID instead",
);

// Esc while editing the filter cancels the edit; the next Esc closes.
function onEscape(e: KeyboardEvent) {
	if (editing.value) e.preventDefault();
}

function copyID() {
	void copy(props.status.instanceID);
}
</script>
