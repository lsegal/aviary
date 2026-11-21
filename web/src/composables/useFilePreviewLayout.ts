import { ref, watch } from "vue";

// The Files page Preview panel's width and open state, remembered per
// browser and shared by the file bar's toggle and the split layout.

export const PREVIEW_MIN_WIDTH = 240;
export const EDITOR_MIN_WIDTH = 280;
export const PREVIEW_DEFAULT_WIDTH = 480;
/** How far one arrow key press on the divider moves it. */
export const PREVIEW_KEY_STEP = 16;

const WIDTH_KEY = "aviary.filePreview.width";
const OPEN_KEY = "aviary.filePreview.open";

/** Clamps a preview width so both panes keep their minimum widths. */
export function clampPreviewWidth(value: number, containerWidth: number) {
	const max = Math.max(PREVIEW_MIN_WIDTH, containerWidth - EDITOR_MIN_WIDTH);
	return Math.round(Math.min(Math.max(value, PREVIEW_MIN_WIDTH), max));
}

const width = ref(
	Number(localStorage.getItem(WIDTH_KEY)) || PREVIEW_DEFAULT_WIDTH,
);
const open = ref(localStorage.getItem(OPEN_KEY) !== "0");

watch(width, (value) => localStorage.setItem(WIDTH_KEY, String(value)));
watch(open, (value) => localStorage.setItem(OPEN_KEY, value ? "1" : "0"));

export function useFilePreviewLayout() {
	return {
		width,
		open,
		toggle() {
			open.value = !open.value;
		},
	};
}
