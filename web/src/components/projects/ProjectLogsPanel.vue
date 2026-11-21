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
		<pre ref="body" data-testid="project-logs-lines"
			class="min-h-0 flex-1 overflow-auto px-4 py-2 font-mono text-xs text-gray-600 dark:text-gray-300">{{ props.lines.join("\n") || "No log lines yet." }}</pre>
	</section>
</template>

<script setup lang="ts">
import { nextTick, onMounted, ref, watch } from "vue";

const HEIGHT_KEY = "aviary.projects.logsHeight";
const MIN_HEIGHT = 96;
const DEFAULT_HEIGHT = 224;

const props = defineProps<{
	/** The board's recent log lines, oldest first. */
	lines: string[];
}>();
const emit = defineEmits<{ close: [] }>();

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
