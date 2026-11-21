import { ref, watch } from "vue";

// The desktop sidebar's width and collapsed state, remembered per browser.
// Both are read synchronously when this module loads, before the first
// render, so a reload never flashes the wrong layout.

export const SIDEBAR_MIN_WIDTH = 200;
export const SIDEBAR_MAX_WIDTH = 480;
export const SIDEBAR_DEFAULT_WIDTH = 240;
/** Dragging the edge this far below the minimum collapses the sidebar. */
export const SIDEBAR_SNAP_WIDTH = SIDEBAR_MIN_WIDTH - 40;
/** How far one arrow key press on the resize handle moves the edge. */
export const SIDEBAR_KEY_STEP = 16;

const WIDTH_KEY = "aviary.sidebar.width";
const COLLAPSED_KEY = "aviary.sidebar.collapsed";

export function clampSidebarWidth(value: number): number {
	return Math.round(
		Math.min(Math.max(value, SIDEBAR_MIN_WIDTH), SIDEBAR_MAX_WIDTH),
	);
}

function storedWidth(): number {
	const value = Number(localStorage.getItem(WIDTH_KEY));
	return value ? clampSidebarWidth(value) : SIDEBAR_DEFAULT_WIDTH;
}

const width = ref(storedWidth());
const collapsed = ref(localStorage.getItem(COLLAPSED_KEY) === "1");

watch(width, (value) => localStorage.setItem(WIDTH_KEY, String(value)));
watch(collapsed, (value) =>
	localStorage.setItem(COLLAPSED_KEY, value ? "1" : "0"),
);

export function useSidebarLayout() {
	return {
		width,
		collapsed,
		setWidth(value: number) {
			width.value = clampSidebarWidth(value);
		},
		resetWidth() {
			width.value = SIDEBAR_DEFAULT_WIDTH;
		},
		toggle() {
			collapsed.value = !collapsed.value;
		},
	};
}
