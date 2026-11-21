<template>
	<details class="group max-w-full" data-testid="command-row">
		<summary class="flex cursor-pointer list-none items-center gap-2 rounded-lg px-2 py-1 font-mono text-xs text-gray-700 hover:bg-gray-50 dark:text-gray-300 dark:hover:bg-gray-800/60">
			<CommandLineIcon aria-hidden="true" class="h-4 w-4 shrink-0 text-gray-400" />
			<span class="min-w-0 flex-1 truncate" :title="command">{{ command || "command" }}</span>
			<span data-testid="command-status" :data-status="status" :title="badgeTitle"
				class="flex shrink-0 items-center gap-1 rounded-full px-2 py-0.5 font-sans text-[11px] font-medium"
				:class="BADGE_CLASS[status]">
				<span v-if="status === 'running'" aria-hidden="true" class="h-1.5 w-1.5 animate-pulse rounded-full bg-amber-500" />
				<CheckIcon v-else-if="status === 'ok'" aria-hidden="true" class="h-3 w-3" />
				<XMarkIcon v-else aria-hidden="true" class="h-3 w-3" />
				{{ badgeText }}
			</span>
		</summary>
		<div class="mt-1 space-y-1.5 pl-8">
			<pre class="whitespace-pre-wrap break-all font-mono text-xs text-gray-600 dark:text-gray-400">$ {{ command }}</pre>
			<pre class="max-h-64 overflow-auto rounded-lg bg-slate-950 p-3 font-mono text-xs text-slate-200">{{ output || (running ? "Running…" : "No output") }}</pre>
		</div>
	</details>
</template>

<script setup lang="ts">
import CheckIcon from "@heroicons/vue/20/solid/CheckIcon";
import XMarkIcon from "@heroicons/vue/20/solid/XMarkIcon";
import CommandLineIcon from "@heroicons/vue/24/outline/CommandLineIcon";
import { computed } from "vue";

// One shell command in a transcript: a single row whose badge resolves in
// place from "running" to "ok" or "failed", with its output on expand.
const props = defineProps<{
	command: string;
	output: string;
	exitCode: number | null;
	running: boolean;
	failed: boolean;
}>();

const BADGE_CLASS = {
	running:
		"bg-amber-50 text-amber-700 dark:bg-amber-950/50 dark:text-amber-300",
	ok: "bg-green-50 text-green-700 dark:bg-green-950/50 dark:text-green-300",
	failed: "bg-red-50 text-red-700 dark:bg-red-950/50 dark:text-red-300",
};

const status = computed(() =>
	props.running ? "running" : props.failed ? "failed" : "ok",
);
const failedCode = computed(() =>
	status.value === "failed" && props.exitCode ? props.exitCode : null,
);
const badgeText = computed(() =>
	failedCode.value === null
		? status.value
		: `failed · exit ${failedCode.value}`,
);
const badgeTitle = computed(() => {
	switch (status.value) {
		case "running":
			return "Running";
		case "ok":
			return "Succeeded";
		default:
			return failedCode.value === null
				? "Failed"
				: `Failed with exit code ${failedCode.value}`;
	}
});
</script>
