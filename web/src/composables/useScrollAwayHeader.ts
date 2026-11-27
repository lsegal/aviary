import { onBeforeUnmount, onMounted, type Ref, ref, watch } from "vue";

/** How long after a wheel or touch move a scroll still counts as the user's. */
const GESTURE_MS = 400;

// Only the shared page header's breadcrumb bar stays pinned; the title row
// scrolls away with the page (#1437). The page's main column scrolls by the
// title row's height at most: the page below the header is sized to what
// the breadcrumb bar leaves, so each page keeps its own scroll areas, and
// the breadcrumb bar is sticky. A user's scroll inside one of the page's
// scroll areas first scrolls the column until the title row is gone, so
// the header leaves as the content moves, then scrolls the area itself.
// Scrolling back up past an area's top reveals the header again, in the
// same wheel gesture rather than only the next one. Scrolls a page makes itself, such as
// following new log lines, leave the header alone.
export function useScrollAwayHeader(
	main: Ref<HTMLElement | null | undefined>,
	enabled: Ref<boolean>,
) {
	/** The page's height below the header: what the breadcrumb bar leaves. */
	const pageHeight = ref<number | null>(null);
	const last = new WeakMap<Element, number>();
	let gestureAt = 0;
	let observer: ResizeObserver | undefined;
	let observed: Element | null = null;

	function crumbBar(): HTMLElement | null {
		return (
			main.value?.querySelector<HTMLElement>(
				'[data-testid="settings-crumb-bar"]',
			) ?? null
		);
	}

	function measure() {
		const bar = crumbBar();
		if (bar !== observed) {
			if (observed) observer?.unobserve(observed);
			if (bar) observer?.observe(bar);
			observed = bar;
		}
		const col = main.value;
		pageHeight.value =
			bar && col ? Math.max(0, col.clientHeight - bar.offsetHeight) : null;
	}

	// Only a downward wheel or a touch drag hides the header: a click or a
	// key that makes the page scroll itself, such as a "More below" pill
	// jumping to the end, must not.
	function onGesture(e: Event) {
		if (e instanceof WheelEvent && e.deltaY <= 0) return;
		gestureAt = Date.now();
	}

	function onScroll(e: Event) {
		const col = main.value;
		const area = e.target;
		if (!col || !(area instanceof Element) || area === col) return;
		const prev = last.get(area) ?? 0;
		const top = area.scrollTop;
		last.set(area, top);
		const down = top - prev;
		if (!enabled.value || down <= 0 || Date.now() - gestureAt > GESTURE_MS)
			return;
		const left = col.scrollHeight - col.clientHeight - col.scrollTop;
		const move = Math.min(down, left);
		if (move < 1) return;
		// The column takes the scroll the area just made, so the content
		// stays where the user put it while the title row leaves.
		col.scrollTop += move;
		area.scrollTop = top - move;
		last.set(area, area.scrollTop);
	}

	/** The scroll area under the pointer that scrolls vertically, if any. */
	function scrollArea(from: EventTarget | null, col: HTMLElement) {
		for (
			let el = from instanceof Element ? from : null;
			el && el !== col;
			el = el.parentElement
		) {
			if (el.scrollHeight <= el.clientHeight) continue;
			const { overflowY } = getComputedStyle(el);
			if (overflowY === "auto" || overflowY === "scroll") return el;
		}
		return null;
	}

	// Chrome keeps a wheel gesture on the area it started in, so a flick up
	// stops at the area's top; the rest of it reveals the header.
	function onWheel(e: WheelEvent) {
		const col = main.value;
		if (!col || !enabled.value || e.deltaY >= 0 || col.scrollTop <= 0) return;
		if (e.ctrlKey || e.shiftKey) return;
		const area = scrollArea(e.target, col);
		if (!area || area.scrollTop > 0) return;
		e.preventDefault();
		const line = e.deltaMode === WheelEvent.DOM_DELTA_LINE ? 16 : 1;
		const page = e.deltaMode === WheelEvent.DOM_DELTA_PAGE ? col.clientHeight : 1;
		col.scrollTop += e.deltaY * line * page;
	}

	const GESTURES = ["wheel", "touchmove"] as const;

	onMounted(() => {
		observer = new ResizeObserver(measure);
		const col = main.value;
		if (!col) return;
		observer.observe(col);
		measure();
		col.addEventListener("scroll", onScroll, { capture: true, passive: true });
		for (const g of GESTURES)
			col.addEventListener(g, onGesture, { capture: true, passive: true });
		col.addEventListener("wheel", onWheel, { passive: false });
	});

	// The header comes and goes with the route.
	watch(enabled, () => queueMicrotask(measure), { flush: "post" });

	onBeforeUnmount(() => {
		observer?.disconnect();
		const col = main.value;
		if (!col) return;
		col.removeEventListener("scroll", onScroll, { capture: true });
		for (const g of GESTURES)
			col.removeEventListener(g, onGesture, { capture: true });
		col.removeEventListener("wheel", onWheel);
	});

	return { pageHeight };
}
