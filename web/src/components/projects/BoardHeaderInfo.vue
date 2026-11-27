<template>
	<HeaderInfoPopover v-if="status.instanceID" testid="instance-tag" label="Instance ID">
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
	<HeaderInfoPopover testid="board-info" label="Board details" @escape-key-down="onEscape">
		<template #icon><InformationCircleIcon aria-hidden="true" class="h-4 w-4" /></template>
		<dl data-testid="board-subtitle" class="grid grid-cols-[auto_minmax(0,1fr)] items-baseline gap-x-3 gap-y-1.5 text-xs">
			<dt class="text-gray-500 dark:text-gray-400">Issues</dt>
			<dd class="min-w-0 break-words">
				<template v-if="manual">Manual: no issue watching</template>
				<IssueFilterEditor v-else-if="filterTarget" v-model:editing="editing" :target="filterTarget"
					:filter="status.issueFilters[filterTarget] ?? status.issueFilter"
					:custom="status.customFilters.includes(filterTarget)" @saved="emit('saved')" />
				<template v-else-if="status.issueFilter">Issues matching <code data-testid="issue-filter" class="font-mono">{{ status.issueFilter }}</code></template>
				<template v-else>All open issues</template>
			</dd>
			<dt class="text-gray-500 dark:text-gray-400">Mode</dt>
			<dd data-testid="board-mode">{{ manual ? "Manual" : "Watch" }}</dd>
			<template v-if="status.delivery && !manual">
				<dt class="text-gray-500 dark:text-gray-400">Delivery</dt>
				<dd data-testid="board-delivery">{{ status.delivery }}</dd>
			</template>
			<template v-if="status.lastChecked || status.stale">
				<dt class="text-gray-500 dark:text-gray-400">Checked</dt>
				<dd data-testid="board-checked">
					<template v-if="status.lastChecked">{{ age(status.lastChecked, now) }} ago</template>
					<span v-if="status.stale" data-testid="stale-indicator" class="text-gray-400 dark:text-gray-500"
						title="Showing the state saved before the last restart until the first live poll confirms it">
						· refreshing…<template v-if="status.cachedAt"> last updated {{ age(status.cachedAt, now) }} ago</template>
					</span>
				</dd>
			</template>
			<template v-if="status.interval">
				<dt class="text-gray-500 dark:text-gray-400">Polls</dt>
				<dd data-testid="board-interval">every {{ status.interval }}</dd>
			</template>
		</dl>
	</HeaderInfoPopover>
</template>

<script setup lang="ts">
// The Code Loop board header's icons beside its status chips (#1570): a tag
// opening the instance ID with a copy button, and an info icon opening what
// the board watches (the issue filter and its editor from #1213, the mode
// from #1375, delivery, the last check and the poll interval), in place of
// the instance badge and subtitle that took up a phone's header.
import ClipboardDocumentIcon from "@heroicons/vue/24/outline/ClipboardDocumentIcon";
import InformationCircleIcon from "@heroicons/vue/24/outline/InformationCircleIcon";
import TagIcon from "@heroicons/vue/24/outline/TagIcon";
import { ref } from "vue";
import { age, type ProjectStatus } from "../../lib/projects";
import HeaderInfoPopover from "../ui/HeaderInfoPopover.vue";
import IssueFilterEditor from "./IssueFilterEditor.vue";

const props = defineProps<{
	status: ProjectStatus;
	manual: boolean;
	/** The project whose filter the editor changes; empty on aggregate boards. */
	filterTarget: string;
	now: number;
}>();
const emit = defineEmits<{
	/** The issue filter was saved or reset. */
	saved: [];
}>();

const editing = ref(false);
const copyStatus = ref("");

// Esc while editing the filter cancels the edit; the next Esc closes.
function onEscape(e: KeyboardEvent) {
	if (editing.value) e.preventDefault();
}

async function copyID() {
	try {
		await navigator.clipboard.writeText(props.status.instanceID);
		copyStatus.value = "Copied";
	} catch {
		copyStatus.value = "Couldn't copy; select the ID instead";
	}
}
</script>
