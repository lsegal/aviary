import { onBeforeUnmount, onMounted, type Ref, ref, watch } from "vue";

/** How long after a wheel or touch move a scroll still counts as the user's. */
const GESTURE_MS = 400;

// Only the shared page header's breadcrumb bar stays pinned; the title row
// scrolls away with the page (#1437). Each page keeps its own scroll areas
// and the page fills what the header leaves, so the title row leaves by
// sliding up under the breadcrumb bar while the page grows into its room:
// a docked panel or composer at the page's bottom stays in view.
//
// A user's downward scroll inside one of the page's scroll areas first
// hides the title row, then scrolls the area, so the content moves with the
// user's hand and nothing jumps. Scrolling up past an area's top brings the
// title row back. On a page with nothing to scroll, the wheel or a drag
// moves the title row directly. Scrolls a page makes itself, such as
// following new log lines or a "More below" pill jumping to the end, leave
// the header alone.
export function useScrollAwayHeader(
	main: Ref<HTMLElement | null | undefined>,
	enabled: Ref<boolean>,
) {
	/** How far the title row has slid under the breadcrumb bar, in pixels. */
	const offset = ref(0);
	const last = new WeakMap<Element, number>();
	let gestureAt = 0;
	let touchY: number | null = null;

	/** The title row's full height: as far as it can slide. */
	function range(): number {
		const row = main.value?.querySelector<HTMLElement>(
			'[data-testid="settings-title-row"]',
		);
		return row?.offsetHeight ?? 0;
	}

	/** Slides the title row by delta, returning how much it moved. */
	function slide(delta: number): number {
		const next = Math.min(range(), Math.max(0, offset.value + delta));
		const moved = next - offset.value;
		offset.value = next;
		return moved;
	}

	/** The page's scroll area under the pointer, if any. */
	function scrollArea(from: EventTarget | null): Element | null {
		const col = main.value;
		for (
			let el = from instanceof Element ? from : null;
			el && el !== col;
			el = el.parentElement
		) {
			if (el.scrollHeight <= el.clientHeight + 1) continue;
			const { overflowY } = getComputedStyle(el);
			if (overflowY === "auto" || overflowY === "scroll") return el;
		}
		return null;
	}

	const atBottom = (el: Element) =>
		el.scrollTop + el.clientHeight >= el.scrollHeight - 1;

	function onScroll(e: Event) {
		const area = e.target;
		if (!(area instanceof Element) || area === main.value) return;
		const prev = last.get(area) ?? area.scrollTop;
		const top = area.scrollTop;
		last.set(area, top);
		const down = top - prev;
		if (!enabled.value || down <= 0 || Date.now() - gestureAt > GESTURE_MS)
			return;
		const moved = slide(down);
		if (moved < 1) return;
		// The title row takes the scroll the area just made, so the content
		// stays where the user put it while the title row leaves.
		area.scrollTop = top - moved;
		last.set(area, area.scrollTop);
	}

	/**
	 * Moves the title row for a gesture the page's scroll areas can't use:
	 * up at an area's top, or down where nothing scrolls further. Returns
	 * whether the gesture was used.
	 */
	function useGesture(target: EventTarget | null, delta: number): boolean {
		const area = scrollArea(target);
		// The area's position before the gesture scrolls it, for onScroll.
		if (area && !last.has(area)) last.set(area, area.scrollTop);
		if (!enabled.value || delta === 0) return false;
		if (delta < 0) {
			if (offset.value <= 0 || (area && area.scrollTop > 0)) return false;
		} else if (offset.value >= range() || (area && !atBottom(area))) {
			return false;
		}
		return Math.abs(slide(delta)) > 0;
	}

	function onWheel(e: WheelEvent) {
		if (e.ctrlKey || e.shiftKey) return;
		if (e.deltaY > 0) gestureAt = Date.now();
		const unit =
			e.deltaMode === WheelEvent.DOM_DELTA_LINE
				? 16
				: e.deltaMode === WheelEvent.DOM_DELTA_PAGE
					? (main.value?.clientHeight ?? 0)
					: 1;
		if (useGesture(e.target, e.deltaY * unit)) e.preventDefault();
	}

	function onTouchStart(e: TouchEvent) {
		touchY = e.touches.length === 1 ? e.touches[0].clientY : null;
	}

	function onTouchMove(e: TouchEvent) {
		gestureAt = Date.now();
		if (touchY === null || e.touches.length !== 1) return;
		const y = e.touches[0].clientY;
		// A finger moving up scrolls the page down.
		const used = useGesture(e.target, touchY - y);
		touchY = y;
		if (used && e.cancelable) e.preventDefault();
	}

	function onTouchEnd() {
		touchY = null;
	}

	// Tabbing to one of the title row's actions brings the row back.
	function onFocusIn(e: Event) {
		if (
			e.target instanceof Element &&
			e.target.closest('[data-testid="settings-title-row"]')
		)
			offset.value = 0;
	}

	const listeners: [string, EventListener, AddEventListenerOptions][] = [
		["scroll", onScroll as EventListener, { capture: true, passive: true }],
		["wheel", onWheel as EventListener, { passive: false }],
		["touchstart", onTouchStart as EventListener, { passive: true }],
		["touchmove", onTouchMove as EventListener, { passive: false }],
		["touchend", onTouchEnd, { passive: true }],
		["touchcancel", onTouchEnd, { passive: true }],
		["focusin", onFocusIn, { passive: true }],
	];

	onMounted(() => {
		const col = main.value;
		if (!col) return;
		for (const [type, fn, opts] of listeners) col.addEventListener(type, fn, opts);
	});

	// A new page, or a page without the header, starts with it shown.
	watch(enabled, () => {
		offset.value = 0;
	});

	onBeforeUnmount(() => {
		const col = main.value;
		if (!col) return;
		for (const [type, fn, opts] of listeners)
			col.removeEventListener(type, fn, { capture: opts.capture });
	});

	return { offset };
}
