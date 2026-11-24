<template>
	<ol ref="listEl" class="relative space-y-1" :data-dragging="drag ? 'true' : undefined">
		<!-- The landing spot while a row is dragged. -->
		<li v-if="drag" aria-hidden="true" data-testid="sortable-placeholder"
			class="pointer-events-none absolute inset-x-0 !mt-0 rounded-lg border-2 border-dashed border-blue-400/70 bg-blue-50/60 dark:border-blue-500/60 dark:bg-blue-950/30"
			:class="settleTransition"
			:style="{ top: `${placeholder.top}px`, height: `${placeholder.height}px` }" />
		<li v-for="(item, i) in items" :key="item.key" :data-key="item.key" v-bind="rowAttrs?.(item)"
			class="relative select-none [-webkit-touch-callout:none]" :class="rowClass(item.key)" :style="rowStyle(i)"
			@pointerdown="onPointerDown($event, item.key)" @contextmenu="onContextMenu">
			<slot :item="item" :index="i" :dragging="drag?.key === item.key"
				:handle="{ 'data-sortable-handle': '', onKeydown: (e: KeyboardEvent) => onHandleKey(e, item.key) }" />
		</li>
	</ol>
</template>

<script setup lang="ts" generic="T extends { key: string }">
// A list whose rows reorder by dragging (#1015). A row's handle, the element
// bound to the slot's `handle`, starts a drag with the mouse; on touch, a
// long press on the handle or anywhere on the row outside its buttons does,
// so a swipe still scrolls. While dragging, the row lifts and follows the
// pointer, the other rows slide out of the way around a placeholder at the
// landing spot, and the list's scroll container scrolls near its edges.
// Dropping settles the row into place, then emits the new order. The
// handle's Up and Down arrow keys move its row one place. Motion follows
// prefers-reduced-motion.
import { computed, nextTick, onBeforeUnmount, ref } from "vue";

const props = defineProps<{
	items: T[];
	/** Extra attributes for an item's row. */
	rowAttrs?: (item: T) => Record<string, unknown>;
}>();

const emit = defineEmits<{
	/** The item keys in their new order, after a drag or arrow key. */
	reorder: [keys: string[], moved: string];
}>();

/** How long a touch rests before it picks up the row. */
const LONG_PRESS_MS = 350;
/** How far a pointer moves before a press counts as a swipe or a drag. */
const SLOP_PX = 6;
/** The band at the scroll container's edges that scrolls it. */
const EDGE_PX = 48;
/** The fastest autoscroll, in pixels per frame. */
const MAX_SCROLL_PX = 14;
const SETTLE_MS = 180;

interface Drag {
	key: string;
	from: number;
	/** Rows' tops and heights in list coordinates when the drag began. */
	tops: number[];
	heights: number[];
	/** The gap between rows. */
	gap: number;
	/** Where in the row the pointer grabbed it. */
	grabY: number;
	pointerY: number;
	/** The dragged row's offset from its own place. */
	offset: number;
	to: number;
}

const listEl = ref<HTMLElement | null>(null);
const drag = ref<Drag | null>(null);
/** The dropped row is sliding into its place. */
const settling = ref(false);
/** Transitions are off for the frame the new order renders. */
const instant = ref(false);
let cleanup: (() => void) | null = null;

const reducedMotion = () =>
	window.matchMedia?.("(prefers-reduced-motion: reduce)").matches ?? false;

const settleTransition = computed(() =>
	instant.value
		? ""
		: "motion-safe:transition-[top,transform] motion-safe:duration-150 motion-safe:ease-out",
);

/** The landing spot, in list coordinates. */
const placeholder = computed(() => {
	const d = drag.value;
	if (!d) return { top: 0, height: 0 };
	const height = d.heights[d.from];
	const top =
		d.to > d.from ? d.tops[d.to] + d.heights[d.to] - height : d.tops[d.to];
	return { top, height };
});

function rowClass(key: string): string {
	const d = drag.value;
	if (d?.key === key) {
		const lift = settling.value
			? ""
			: "shadow-xl ring-2 ring-blue-500 motion-safe:scale-[1.02] motion-safe:-rotate-1";
		return `z-10 rounded-lg ${lift} ${settling.value ? settleTransition.value : "motion-safe:transition-[box-shadow,scale,rotate] motion-safe:duration-150"}`;
	}
	return settleTransition.value;
}

function rowStyle(i: number): Record<string, string> | undefined {
	const d = drag.value;
	if (!d) return undefined;
	if (i === d.from) return { transform: `translateY(${d.offset}px)` };
	const slot = d.heights[d.from] + d.gap;
	let shift = 0;
	if (d.to > d.from && i > d.from && i <= d.to) shift = -slot;
	if (d.to < d.from && i >= d.to && i < d.from) shift = slot;
	return shift ? { transform: `translateY(${shift}px)` } : undefined;
}

function rowEls(): HTMLElement[] {
	return [
		...(listEl.value?.querySelectorAll<HTMLElement>(":scope > li[data-key]") ??
			[]),
	];
}

/** The nearest ancestor that scrolls vertically. */
function scroller(): HTMLElement | null {
	for (let el = listEl.value?.parentElement; el; el = el.parentElement) {
		const { overflowY } = getComputedStyle(el);
		if (/(auto|scroll)/.test(overflowY) && el.scrollHeight > el.clientHeight)
			return el;
	}
	return null;
}

function onContextMenu(e: Event) {
	// A long press would otherwise open the touch context menu.
	if (drag.value || pending) e.preventDefault();
}

let pending = false;

function onPointerDown(e: PointerEvent, key: string) {
	if (drag.value || e.button !== 0) return;
	const target = e.target as Element;
	const onHandle = !!target.closest("[data-sortable-handle]");
	const touch = e.pointerType === "touch";
	if (!onHandle && (!touch || target.closest("button, a, input"))) return;
	if (!touch) e.preventDefault();
	const startX = e.clientX;
	const startY = e.clientY;
	let lastY = startY;
	pending = true;
	let timer = 0;

	const release = () => {
		pending = false;
		clearTimeout(timer);
		window.removeEventListener("pointermove", onMove);
		window.removeEventListener("pointerup", release);
		window.removeEventListener("pointercancel", release);
	};
	const begin = () => {
		release();
		start(key, startY, lastY);
	};
	const onMove = (ev: PointerEvent) => {
		lastY = ev.clientY;
		const moved = Math.hypot(ev.clientX - startX, ev.clientY - startY);
		if (moved < SLOP_PX) return;
		// A touch that moves before the long press is a swipe.
		if (touch) release();
		else begin();
	};
	window.addEventListener("pointermove", onMove);
	window.addEventListener("pointerup", release);
	window.addEventListener("pointercancel", release);
	if (touch)
		timer = window.setTimeout(() => {
			navigator.vibrate?.(10);
			begin();
		}, LONG_PRESS_MS);
}

function start(key: string, grabClientY: number, pointerY: number) {
	const list = listEl.value;
	const from = props.items.findIndex((item) => item.key === key);
	if (!list || from < 0) return;
	const listTop = list.getBoundingClientRect().top;
	const rects = rowEls().map((el) => el.getBoundingClientRect());
	const tops = rects.map((r) => r.top - listTop);
	const heights = rects.map((r) => r.height);
	drag.value = {
		key,
		from,
		tops,
		heights,
		gap: rects.length > 1 ? rects[1].top - rects[0].bottom : 0,
		grabY: grabClientY - rects[from].top,
		pointerY,
		offset: 0,
		to: from,
	};
	update();

	const container = scroller();
	let frame = 0;
	const tick = () => {
		frame = requestAnimationFrame(tick);
		const d = drag.value;
		if (!container || !d || settling.value) return;
		const box = container.getBoundingClientRect();
		const above = box.top + EDGE_PX - d.pointerY;
		const below = d.pointerY - (box.bottom - EDGE_PX);
		const speed = (depth: number) =>
			Math.ceil(MAX_SCROLL_PX * Math.min(1, depth / EDGE_PX));
		const before = container.scrollTop;
		if (above > 0) container.scrollTop -= speed(above);
		else if (below > 0) container.scrollTop += speed(below);
		if (container.scrollTop !== before) update();
	};
	frame = requestAnimationFrame(tick);

	const onMove = (ev: PointerEvent) => {
		if (!drag.value || settling.value) return;
		drag.value.pointerY = ev.clientY;
		update();
	};
	// Keeps a touch drag from scrolling the page instead.
	const onTouchMove = (ev: TouchEvent) => ev.preventDefault();
	const onKey = (ev: KeyboardEvent) => {
		if (ev.key !== "Escape" || !drag.value) return;
		ev.preventDefault();
		ev.stopPropagation();
		drag.value.to = drag.value.from;
		drop();
	};
	const onUp = () => drop();
	const onCancel = () => {
		if (drag.value) drag.value.to = drag.value.from;
		drop();
	};
	window.addEventListener("pointermove", onMove);
	window.addEventListener("pointerup", onUp);
	window.addEventListener("pointercancel", onCancel);
	window.addEventListener("touchmove", onTouchMove, { passive: false });
	window.addEventListener("keydown", onKey, true);
	cleanup = () => {
		cancelAnimationFrame(frame);
		window.removeEventListener("pointermove", onMove);
		window.removeEventListener("pointerup", onUp);
		window.removeEventListener("pointercancel", onCancel);
		window.removeEventListener("touchmove", onTouchMove);
		window.removeEventListener("keydown", onKey, true);
		cleanup = null;
	};
}

/** Moves the dragged row under the pointer and finds its landing spot. */
function update() {
	const d = drag.value;
	const list = listEl.value;
	if (!d || !list) return;
	const listTop = list.getBoundingClientRect().top;
	const height = d.heights[d.from];
	const last = d.tops.length - 1;
	const minTop = -height / 2;
	const maxTop = d.tops[last] + d.heights[last] - height / 2;
	const top = Math.min(
		maxTop,
		Math.max(minTop, d.pointerY - listTop - d.grabY),
	);
	d.offset = top - d.tops[d.from];
	const center = top + height / 2;
	let to = 0;
	d.tops.forEach((t, i) => {
		if (i !== d.from && t + d.heights[i] / 2 < center) to++;
	});
	d.to = to;
}

/** Ends the drop at once, as when the list unmounts while it settles. */
let finishDrop: (() => void) | null = null;

function drop() {
	cleanup?.();
	const d = drag.value;
	if (!d || finishDrop) return;
	let timer = 0;
	finishDrop = () => {
		clearTimeout(timer);
		finishDrop = null;
		commit(d);
	};
	if (reducedMotion()) {
		finishDrop();
		return;
	}
	settling.value = true;
	d.offset = placeholder.value.top - d.tops[d.from];
	timer = window.setTimeout(() => finishDrop?.(), SETTLE_MS);
}

async function commit(d: Drag) {
	// Render the new order with every row already where it shows, so
	// nothing jumps when the transforms clear.
	instant.value = true;
	const keys = props.items.map((item) => item.key);
	const moved = d.to !== d.from;
	if (moved) {
		const [key] = keys.splice(d.from, 1);
		keys.splice(d.to, 0, key);
	}
	drag.value = null;
	settling.value = false;
	if (moved) emit("reorder", keys, d.key);
	await nextTick();
	requestAnimationFrame(() => {
		instant.value = false;
	});
}

function onHandleKey(e: KeyboardEvent, key: string) {
	const delta = e.key === "ArrowUp" ? -1 : e.key === "ArrowDown" ? 1 : 0;
	if (!delta || drag.value) return;
	e.preventDefault();
	const keys = props.items.map((item) => item.key);
	const from = keys.indexOf(key);
	const to = from + delta;
	if (to < 0 || to >= keys.length) return;
	keys.splice(from, 1);
	keys.splice(to, 0, key);
	emit("reorder", keys, key);
}

onBeforeUnmount(() => {
	cleanup?.();
	finishDrop?.();
});
</script>
