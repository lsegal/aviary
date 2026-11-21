<template>
	<!-- The editor (default slot) on the left and the file's previewer on the
	     right. Narrow screens get Edit / Preview tabs instead of a split. -->
	<div ref="container" data-testid="file-preview-split" class="flex min-h-0 min-w-0 flex-1 flex-col md:flex-row">
		<div v-if="previewer && !desktop" role="tablist" aria-label="Editor view" data-testid="file-preview-tabs"
			class="flex shrink-0 gap-1 border-b border-gray-200 bg-white px-4 py-2 dark:border-gray-800 dark:bg-gray-950">
			<button v-for="tab in tabs" :key="tab.key" type="button" role="tab" :aria-selected="mobileTab === tab.key"
				:data-testid="`file-preview-tab-${tab.key}`"
				class="rounded-lg px-3 py-1.5 text-sm font-medium focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
				:class="mobileTab === tab.key
					? 'bg-gray-200 text-gray-900 dark:bg-gray-800 dark:text-white'
					: 'text-gray-600 hover:bg-gray-100 dark:text-gray-400 dark:hover:bg-gray-800/60'"
				@click="mobileTab = tab.key">
				{{ tab.label }}
			</button>
		</div>
		<div v-show="editorShown" data-testid="file-preview-editor-pane" class="flex min-h-0 min-w-0 flex-1 flex-col">
			<slot />
		</div>
		<div v-if="previewer && previewShown" data-testid="file-preview-pane" class="relative flex min-h-0 min-w-0 flex-col"
			:class="desktop ? 'shrink-0 border-l border-gray-200 dark:border-gray-800' : 'flex-1'"
			:style="desktop ? { width: `${effectiveWidth}px` } : undefined">
			<div v-if="desktop" role="separator" tabindex="0" aria-orientation="vertical" aria-label="Resize preview"
				data-testid="file-preview-divider" :aria-valuenow="effectiveWidth" :aria-valuemin="PREVIEW_MIN_WIDTH"
				:aria-valuemax="maxWidth" title="Drag to resize, double-click to reset"
				class="absolute inset-y-0 -left-1 z-10 w-2 cursor-col-resize transition-colors hover:bg-blue-500/30 focus-visible:bg-blue-500/40 focus-visible:outline-none"
				:class="resizing ? 'bg-blue-500/40' : ''"
				@pointerdown="startResize" @dblclick="width = PREVIEW_DEFAULT_WIDTH" @keydown="resizeByKey" />
			<section data-testid="file-preview" :aria-label="`${previewer.label} preview`"
				class="min-h-0 flex-1 overflow-y-auto bg-gray-50 px-4 py-4 sm:px-6 dark:bg-gray-900/60">
				<component :is="previewer.component" :content="previewContent" />
			</section>
		</div>
	</div>
</template>

<script setup lang="ts">
import {
	computed,
	onBeforeUnmount,
	onMounted,
	ref,
	useTemplateRef,
	watch,
} from "vue";
import {
	clampPreviewWidth,
	PREVIEW_DEFAULT_WIDTH,
	PREVIEW_KEY_STEP,
	PREVIEW_MIN_WIDTH,
	useFilePreviewLayout,
} from "../../composables/useFilePreviewLayout";
import { findFilePreviewer } from "./previewers";

const props = defineProps<{ fileName: string; content: string }>();

const PREVIEW_DEBOUNCE_MS = 150;

const { width, open } = useFilePreviewLayout();
const previewer = computed(() => findFilePreviewer(props.fileName));

const desktopQuery = window.matchMedia("(min-width: 768px)");
const desktop = ref(desktopQuery.matches);
const syncDesktop = () => {
	desktop.value = desktopQuery.matches;
};

const tabs = [
	{ key: "edit", label: "Edit" },
	{ key: "preview", label: "Preview" },
] as const;
const mobileTab = ref<"edit" | "preview">("edit");

const previewShown = computed(() =>
	desktop.value ? open.value : mobileTab.value === "preview",
);
const editorShown = computed(
	() => desktop.value || !previewer.value || mobileTab.value === "edit",
);

// The previewer trails typing slightly; opening another file shows it at once.
const previewContent = ref(props.content);
let debounce: ReturnType<typeof setTimeout> | undefined;
watch(
	() => props.content,
	(value) => {
		clearTimeout(debounce);
		debounce = setTimeout(() => {
			previewContent.value = value;
		}, PREVIEW_DEBOUNCE_MS);
	},
);
watch(
	() => props.fileName,
	() => {
		clearTimeout(debounce);
		previewContent.value = props.content;
	},
);

const container = useTemplateRef<HTMLElement>("container");
const containerWidth = ref(0);
const observer = new ResizeObserver(([entry]) => {
	containerWidth.value = entry.contentRect.width;
});
const maxWidth = computed(() =>
	clampPreviewWidth(Number.POSITIVE_INFINITY, containerWidth.value),
);
const effectiveWidth = computed(() =>
	clampPreviewWidth(width.value, containerWidth.value),
);
const setWidth = (value: number) => {
	width.value = clampPreviewWidth(value, containerWidth.value);
};

const resizing = ref(false);
function startResize(e: PointerEvent) {
	if (e.button !== 0) return;
	e.preventDefault();
	const startX = e.clientX;
	const startWidth = effectiveWidth.value;
	resizing.value = true;
	document.body.style.cursor = "col-resize";
	document.body.style.userSelect = "none";
	// The preview is on the right, so dragging left widens it.
	const onMove = (move: PointerEvent) =>
		setWidth(startWidth - (move.clientX - startX));
	const stop = () => {
		window.removeEventListener("pointermove", onMove);
		window.removeEventListener("pointerup", stop);
		resizing.value = false;
		document.body.style.cursor = "";
		document.body.style.userSelect = "";
	};
	window.addEventListener("pointermove", onMove);
	window.addEventListener("pointerup", stop);
}

function resizeByKey(e: KeyboardEvent) {
	const steps: Record<string, number> = {
		ArrowLeft: effectiveWidth.value + PREVIEW_KEY_STEP,
		ArrowRight: effectiveWidth.value - PREVIEW_KEY_STEP,
		Home: maxWidth.value,
		End: PREVIEW_MIN_WIDTH,
	};
	if (!(e.key in steps)) return;
	e.preventDefault();
	setWidth(steps[e.key]);
}

onMounted(() => {
	desktopQuery.addEventListener("change", syncDesktop);
	if (container.value) observer.observe(container.value);
});
onBeforeUnmount(() => {
	desktopQuery.removeEventListener("change", syncDesktop);
	observer.disconnect();
	clearTimeout(debounce);
});
</script>
