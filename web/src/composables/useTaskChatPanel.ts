import { computed, ref, watch } from "vue";

// The chat panel along the bottom of a scheduled task's dashboard: whether it
// shows and how tall it is, remembered per browser and shared by every task.
// Read synchronously when this module loads, so a reload never flashes the
// default layout.

/** The panel's shortest height. */
export const TASK_CHAT_MIN_HEIGHT = 160;
/** The panel's height before it is resized. */
export const TASK_CHAT_DEFAULT_HEIGHT = 320;
/** How much of the viewport's height the panel always leaves to the dashboard. */
export const TASK_CHAT_DASHBOARD_RESERVE = 240;
/** How far one arrow key press on the resize handle moves the edge. */
const KEY_STEP = 16;
/** Below this viewport width the chat opens as a bottom sheet instead. */
const DESKTOP_QUERY = "(min-width: 768px)";

const OPEN_KEY = "aviary.taskChat.open";
const HEIGHT_KEY = "aviary.taskChat.height";

/** The tallest the panel may be in a viewport this tall. */
export function maxTaskChatHeight(viewportHeight: number): number {
	return Math.max(
		TASK_CHAT_MIN_HEIGHT,
		viewportHeight - TASK_CHAT_DASHBOARD_RESERVE,
	);
}

export function clampTaskChatHeight(
	value: number,
	viewportHeight: number,
): number {
	return Math.round(
		Math.min(
			Math.max(value, TASK_CHAT_MIN_HEIGHT),
			maxTaskChatHeight(viewportHeight),
		),
	);
}

const open = ref(localStorage.getItem(OPEN_KEY) !== "false");
// The chosen height as stored; what is shown is clamped to the current
// viewport, so a shorter window never overflows and growing it again
// restores the choice.
const stored = ref(
	Number(localStorage.getItem(HEIGHT_KEY)) || TASK_CHAT_DEFAULT_HEIGHT,
);
const viewport = ref(window.innerHeight);
const desktopQuery = window.matchMedia(DESKTOP_QUERY);
const desktop = ref(desktopQuery.matches);

window.addEventListener("resize", () => {
	viewport.value = window.innerHeight;
});
desktopQuery.addEventListener("change", (e) => {
	desktop.value = e.matches;
});
watch(open, (value) => localStorage.setItem(OPEN_KEY, String(value)));
watch(stored, (value) => localStorage.setItem(HEIGHT_KEY, String(value)));

const height = computed(() =>
	clampTaskChatHeight(stored.value, viewport.value),
);
const maxHeight = computed(() => maxTaskChatHeight(viewport.value));

function setHeight(value: number) {
	stored.value = clampTaskChatHeight(value, viewport.value);
}

/** Drags the panel's top edge: moving up makes it taller. */
function startResize(e: PointerEvent) {
	e.preventDefault();
	const startY = e.clientY;
	const startHeight = height.value;
	document.body.style.cursor = "row-resize";
	document.body.style.userSelect = "none";
	const onMove = (move: PointerEvent) => {
		setHeight(startHeight + startY - move.clientY);
	};
	const stop = () => {
		window.removeEventListener("pointermove", onMove);
		window.removeEventListener("pointerup", stop);
		document.body.style.cursor = "";
		document.body.style.userSelect = "";
	};
	window.addEventListener("pointermove", onMove);
	window.addEventListener("pointerup", stop);
}

/** Arrow keys move the top edge; Home and End jump to the bounds. */
function resizeByKey(e: KeyboardEvent) {
	const steps: Record<string, number> = {
		ArrowUp: height.value + KEY_STEP,
		ArrowDown: height.value - KEY_STEP,
		Home: TASK_CHAT_MIN_HEIGHT,
		End: maxHeight.value,
	};
	if (!(e.key in steps)) return;
	e.preventDefault();
	setHeight(steps[e.key]);
}

export function useTaskChatPanel() {
	return {
		open,
		height,
		maxHeight,
		desktop,
		setHeight,
		startResize,
		resizeByKey,
	};
}
