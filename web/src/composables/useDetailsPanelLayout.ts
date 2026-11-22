import { computed, ref, watch } from "vue";

// The width of the right-hand details drawer (for example a scheduled task
// run's details), remembered per browser and shared by every such drawer.
// Read synchronously when this module loads, so a reload never flashes the
// default width (#878).

/** The drawer's default and minimum width (Tailwind's `w-96`). */
export const DETAILS_PANEL_MIN_WIDTH = 384;
/** The widest the drawer may be, as a fraction of the viewport, so the list
 * behind it stays reachable. */
export const DETAILS_PANEL_MAX_FRACTION = 0.75;
/** How far one arrow key press on the resize handle moves the edge. */
export const DETAILS_PANEL_KEY_STEP = 16;
/** Below this viewport width the drawer keeps its fixed mobile layout. */
const DESKTOP_QUERY = "(min-width: 768px)";

const WIDTH_KEY = "aviary.detailsPanel.width";

/** The widest the drawer may be in a viewport this wide. */
export function maxDetailsPanelWidth(viewportWidth: number): number {
	return Math.max(
		DETAILS_PANEL_MIN_WIDTH,
		Math.floor(viewportWidth * DETAILS_PANEL_MAX_FRACTION),
	);
}

export function clampDetailsPanelWidth(
	value: number,
	viewportWidth: number,
): number {
	return Math.round(
		Math.min(
			Math.max(value, DETAILS_PANEL_MIN_WIDTH),
			maxDetailsPanelWidth(viewportWidth),
		),
	);
}

// The chosen width as stored; what is shown is clamped to the current
// viewport, so a narrower window never overflows and widening it again
// restores the choice.
const stored = ref(
	Number(localStorage.getItem(WIDTH_KEY)) || DETAILS_PANEL_MIN_WIDTH,
);
const viewport = ref(window.innerWidth);
const desktopQuery = window.matchMedia(DESKTOP_QUERY);
const desktop = ref(desktopQuery.matches);
const resizing = ref(false);

window.addEventListener("resize", () => {
	viewport.value = window.innerWidth;
});
desktopQuery.addEventListener("change", (e) => {
	desktop.value = e.matches;
});
watch(stored, (value) => localStorage.setItem(WIDTH_KEY, String(value)));

const width = computed(() =>
	clampDetailsPanelWidth(stored.value, viewport.value),
);
const maxWidth = computed(() => maxDetailsPanelWidth(viewport.value));

function setWidth(value: number) {
	stored.value = clampDetailsPanelWidth(value, viewport.value);
}

/** Drags the drawer's left edge: moving left widens it. */
function startResize(e: PointerEvent) {
	e.preventDefault();
	const startX = e.clientX;
	const startWidth = width.value;
	resizing.value = true;
	document.body.style.cursor = "col-resize";
	document.body.style.userSelect = "none";
	const onMove = (move: PointerEvent) => {
		setWidth(startWidth + startX - move.clientX);
	};
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

/** Arrow keys move the left edge; Home and End jump to the bounds. */
function resizeByKey(e: KeyboardEvent) {
	const steps: Record<string, number> = {
		ArrowLeft: width.value + DETAILS_PANEL_KEY_STEP,
		ArrowRight: width.value - DETAILS_PANEL_KEY_STEP,
		Home: DETAILS_PANEL_MIN_WIDTH,
		End: maxWidth.value,
	};
	if (!(e.key in steps)) return;
	e.preventDefault();
	setWidth(steps[e.key]);
}

export function useDetailsPanelLayout() {
	return {
		width,
		maxWidth,
		desktop,
		resizing,
		/** The drawer's inline style: the chosen width on desktop only. */
		style: computed(() => (desktop.value ? { width: `${width.value}px` } : {})),
		setWidth,
		resetWidth() {
			stored.value = DETAILS_PANEL_MIN_WIDTH;
		},
		startResize,
		resizeByKey,
	};
}
