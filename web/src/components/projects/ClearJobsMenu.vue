<template>
	<DropdownMenuRoot>
		<DropdownMenuTrigger asChild>
			<button type="button" data-testid="clear-jobs-menu" aria-label="Clear finished jobs…" title="Clear finished jobs…"
				:class="HEADER_ICON_CLS">
				<TrashIcon aria-hidden="true" class="h-5 w-5" />
			</button>
		</DropdownMenuTrigger>
		<DropdownMenuPortal>
			<DropdownMenuContent align="end" :side-offset="6" data-testid="clear-jobs-items"
				class="z-50 min-w-60 rounded-xl border border-gray-200 bg-white p-1 shadow-lg dark:border-gray-700 dark:bg-gray-900">
				<DropdownMenuLabel class="px-3 py-1.5 text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
					{{ scopeLabel }}
				</DropdownMenuLabel>
				<DropdownMenuItem v-for="option in options" :key="option.which" :disabled="option.count === 0"
					:data-testid="`clear-jobs-${option.which}`" :class="itemClass" @select="pending = option">
					<span class="flex-1">{{ option.label }}</span>
					<span class="tabular-nums text-xs text-gray-500 dark:text-gray-400">{{ option.count }}</span>
				</DropdownMenuItem>
				<p class="px-3 py-1.5 text-xs text-gray-500 dark:text-gray-400">
					Running, queued and paused jobs are never cleared.
				</p>
			</DropdownMenuContent>
		</DropdownMenuPortal>
	</DropdownMenuRoot>
	<ConfirmDialog :open="pending !== null" :title="pending ? `${pending.label}?` : ''" :action="clear"
		confirm-label="Clear" busy-label="Clearing…" test-id="clear-jobs-confirm" @update:open="(v) => { if (!v) pending = null }">
		<p>
			This removes the {{ pending?.noun }} {{ scopeText }} from the board and
			Code Loop's job store, with their transcripts. GitHub is not touched, and a later run of the same issue still shows.
		</p>
	</ConfirmDialog>
</template>

<script setup lang="ts">
// The board header's clear menu (#1460): clears the board's done jobs, its
// failed jobs, or both, from the board and the server-side job store, for
// the board's projects. A single card is cleared from the card itself.

import TrashIcon from "@heroicons/vue/24/outline/TrashIcon";
import {
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuLabel,
	DropdownMenuPortal,
	DropdownMenuRoot,
	DropdownMenuTrigger,
} from "radix-vue";
import { computed, ref } from "vue";
import { useMCP } from "../../composables/useMCP";
import { HEADER_ICON_CLS } from "../../lib/headerActions";
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
}>();
const emit = defineEmits<{ cleared: [count: number] }>();

const { callTool } = useMCP();
const pending = ref<ClearOption | null>(null);

const itemClass =
	"flex cursor-pointer items-center gap-2 rounded-lg px-3 py-2 text-sm text-gray-700 outline-none data-[disabled]:cursor-not-allowed data-[highlighted]:bg-gray-100 data-[disabled]:opacity-50 dark:text-gray-200 dark:data-[highlighted]:bg-gray-800";

const single = computed(() => props.targets.length === 1);
const scopeLabel = computed(() =>
	single.value ? "Clear this project's jobs" : "Clear across these projects",
);
const scopeText = computed(() =>
	single.value ? `of ${props.targets[0]}` : "across the board's projects",
);
const options = computed<ClearOption[]>(() => [
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
