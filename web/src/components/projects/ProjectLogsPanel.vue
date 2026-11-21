<template>
	<section data-testid="project-logs" aria-label="Logs"
		class="relative flex shrink-0 flex-col border-t border-gray-200 bg-gray-50 dark:border-gray-800 dark:bg-gray-900"
		:style="{ height: `${height}px` }">
		<div data-testid="project-logs-resize" role="separator" aria-orientation="horizontal" aria-label="Resize logs"
			class="absolute inset-x-0 -top-1 z-20 h-2 cursor-row-resize hover:bg-blue-500/30"
			@pointerdown="startResize" />
		<header class="flex items-center gap-2 border-b border-gray-200 px-4 py-1.5 dark:border-gray-800">
			<h2 class="flex-1 text-sm font-semibold text-gray-900 dark:text-white">Logs</h2>
			<button type="button" aria-label="Close logs" title="Close"
				class="rounded-lg px-2 py-0.5 text-gray-500 hover:bg-gray-100 hover:text-gray-800 dark:hover:bg-gray-800 dark:hover:text-gray-200"
				@click="emit('close')">✕</button>
		</header>
		<div ref="body" data-testid="project-logs-lines"
			class="min-h-0 flex-1 overflow-y-auto overflow-x-hidden px-4 py-2 font-mono text-xs text-gray-600 dark:text-gray-300">
			<div v-for="(entry, i) in entries" :key="i" data-testid="project-logs-entry" class="flex gap-2">
				<span v-if="entry.time" class="shrink-0 text-gray-400 dark:text-gray-500">{{ entry.time }}</span>
				<span class="min-w-0 flex-1 whitespace-pre-wrap wrap-anywhere">{{ entry.text }}</span>
			</div>
			<p v-if="entries.length === 0">No log lines yet.</p>
		</div>
	</section>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from "vue";

const HEIGHT_KEY = "aviary.projects.logsHeight";
const MIN_HEIGHT = 96;
const DEFAULT_HEIGHT = 224;
// A log entry starts with its time, optionally preceded by its date.
const ENTRY_START = /^((?:\d{4}-\d{2}-\d{2}[ T])?\d{1,2}:\d{2}:\d{2}(?:\.\d+)?) (.*)$/s;

const props = defineProps<{
	/** The board's recent log lines, oldest first. */
	lines: string[];
}>();
const emit = defineEmits<{ close: [] }>();

// Lines without a timestamp continue the entry before them, so a multi-line
// entry renders as one block indented after its time.
const entries = computed(() => {
	const out: { time: string; text: string }[] = [];
	for (const line of props.lines) {
		const start = ENTRY_START.exec(line);
		const last = out.at(-1);
		if (start) out.push({ time: start[1], text: start[2] });
		else if (last) last.text += `\n${line}`;
		else out.push({ time: "", text: line });
	}
	return out;
});

const body = ref<HTMLElement | null>(null);
const height = ref(
	clampHeight(Number(localStorage.getItem(HEIGHT_KEY)) || DEFAULT_HEIGHT),
);

function clampHeight(value: number): number {
	return Math.min(
		Math.max(value, MIN_HEIGHT),
		Math.max(MIN_HEIGHT, window.innerHeight - 240),
	);
}

function startResize(e: PointerEvent) {
	e.preventDefault();
	const startY = e.clientY;
	const startHeight = height.value;
	const onMove = (move: PointerEvent) => {
		height.value = clampHeight(startHeight + startY - move.clientY);
	};
	const onUp = () => {
		window.removeEventListener("pointermove", onMove);
		window.removeEventListener("pointerup", onUp);
		localStorage.setItem(HEIGHT_KEY, String(height.value));
	};
	window.addEventListener("pointermove", onMove);
	window.addEventListener("pointerup", onUp);
}

// New lines stream in with each status update; keep the newest in view.
async function pinToBottom() {
	await nextTick();
	if (body.value) body.value.scrollTop = body.value.scrollHeight;
}
watch(() => props.lines.join("\n"), pinToBottom);
watch(height, pinToBottom);
onMounted(pinToBottom);
</script>
