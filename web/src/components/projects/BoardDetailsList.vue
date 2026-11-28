<template>
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
</template>

<script setup lang="ts">
// What a Code Loop board watches (#1570): the issue filter and its editor
// from #1213, the mode from #1375, delivery, the last check and the poll
// interval. The board header's details popover shows it, and so does its
// "…" menu once the details icon has moved there on a phone (#1576).
import { age, type ProjectStatus } from "../../lib/projects";
import IssueFilterEditor from "./IssueFilterEditor.vue";

defineProps<{
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
/** Whether the issue filter is being edited. */
const editing = defineModel<boolean>("editing", { default: false });
</script>
