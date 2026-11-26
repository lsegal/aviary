import { onBeforeUnmount, onMounted, type Ref, ref, watch } from "vue";

/** How long after a wheel, touch or key a scroll still counts as the user's. */
const GESTURE_MS = 600;

// Only the shared page header's breadcrumb bar stays pinned; the title row
// scrolls away with the page (#1437). The page's main column scrolls by the
// title row's height at most: the page below the header is sized to what
// the breadcrumb bar leaves, so each page keeps its own scroll areas, and
// the breadcrumb bar is sticky. A user's scroll inside one of the page's
// scroll areas first scrolls the column until the title row is gone, so
// the header leaves as the content moves, then scrolls the area itself.
// Scrolling back up past an area's top reveals the header again through
// the browser's own scroll chaining. Scrolls a page makes itself, such as
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

	function onGesture() {
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

	const GESTURES = ["wheel", "touchmove", "keydown", "pointerdown"] as const;

	onMounted(() => {
		observer = new ResizeObserver(measure);
		const col = main.value;
		if (!col) return;
		observer.observe(col);
		measure();
		col.addEventListener("scroll", onScroll, { capture: true, passive: true });
		for (const g of GESTURES)
			col.addEventListener(g, onGesture, { capture: true, passive: true });
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
	});

	return { pageHeight };
}
