<template>
	<aside data-testid="project-chat" aria-label="Prompt chat"
		class="fixed inset-x-0 bottom-0 z-40 flex h-[70vh] flex-col rounded-t-2xl border-t border-gray-200 bg-white shadow-2xl dark:border-gray-800 dark:bg-gray-950 md:relative md:inset-auto md:z-auto md:h-full md:shrink-0 md:rounded-none md:border-l md:border-t-0 md:shadow-none"
		:style="desktop ? { width: `${width}px` } : undefined">
		<div data-testid="project-chat-resize" role="separator" aria-orientation="vertical" aria-label="Resize prompt chat"
			class="absolute inset-y-0 -left-1 z-20 hidden w-2 cursor-col-resize hover:bg-blue-500/30 md:block"
			@pointerdown="startResize" />
		<div class="flex justify-center pt-2 md:hidden" aria-hidden="true">
			<span class="h-1 w-10 rounded-full bg-gray-300 dark:bg-gray-700" />
		</div>
		<header class="flex items-center gap-2 border-b border-gray-200 px-4 py-2.5 dark:border-gray-800">
			<div class="min-w-0 flex-1">
				<h2 class="text-sm font-semibold text-gray-900 dark:text-white">Prompt chat</h2>
				<p class="truncate text-xs text-gray-500 dark:text-gray-400">
					Ask the <b>{{ PROJECTS_AGENT }}</b> agent about this instance's jobs. It can't steer a running job.
				</p>
			</div>
			<button type="button" aria-label="Close prompt chat" title="Close"
				class="rounded-lg px-2 py-1 text-gray-500 hover:bg-gray-100 hover:text-gray-800 dark:hover:bg-gray-800 dark:hover:text-gray-200"
				@click="emit('close')">✕</button>
		</header>
		<div class="min-h-0 flex-1">
			<ChatView ref="chat" :agent="PROJECTS_AGENT" />
		</div>
	</aside>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref } from "vue";
import ChatView from "../../views/ChatView.vue";

/** The built-in Aviary agent behind the prompt chat (config.ProjectsAgentName). */
const PROJECTS_AGENT = "projects";
const WIDTH_KEY = "aviary.projects.chatWidth";
const MIN_WIDTH = 320;
const DEFAULT_WIDTH = 420;

const emit = defineEmits<{ close: [] }>();

const chat = ref<{ sendText: (text: string) => Promise<void> } | null>(null);
const width = ref(
	clampWidth(Number(localStorage.getItem(WIDTH_KEY)) || DEFAULT_WIDTH),
);
const desktopQuery = window.matchMedia("(min-width: 768px)");
const desktop = ref(desktopQuery.matches);

function clampWidth(value: number): number {
	return Math.min(
		Math.max(value, MIN_WIDTH),
		Math.max(MIN_WIDTH, window.innerWidth - 320),
	);
}

function onDesktopChange(e: MediaQueryListEvent) {
	desktop.value = e.matches;
}

function startResize(e: PointerEvent) {
	e.preventDefault();
	const startX = e.clientX;
	const startWidth = width.value;
	const onMove = (move: PointerEvent) => {
		width.value = clampWidth(startWidth + startX - move.clientX);
	};
	const onUp = () => {
		window.removeEventListener("pointermove", onMove);
		window.removeEventListener("pointerup", onUp);
		localStorage.setItem(WIDTH_KEY, String(width.value));
	};
	window.addEventListener("pointermove", onMove);
	window.addEventListener("pointerup", onUp);
}

onMounted(() => desktopQuery.addEventListener("change", onDesktopChange));
onUnmounted(() => desktopQuery.removeEventListener("change", onDesktopChange));

/** Sends a prompt to the agent, as Summarize on a job card does. */
async function send(text: string) {
	await chat.value?.sendText(text);
}

defineExpose({ send });
</script>
