import { onBeforeUnmount, onMounted, type Ref, ref, watch } from "vue";
import { findScrollArea, HeaderSlide } from "../lib/scrollAwayHeader";

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
	const slide = new HeaderSlide<Element>(
		() =>
			main.value?.querySelector<HTMLElement>(
				'[data-testid="settings-title-row"]',
			)?.offsetHeight ?? 0,
		(next) => {
			offset.value = next;
		},
	);

	/** The page's scroll area under the pointer, if any. */
	const areaAt = (target: EventTarget | null) =>
		findScrollArea<Element>(
			target instanceof Element ? target : null,
			main.value ?? null,
			(el) => getComputedStyle(el).overflowY,
		);

	function onScroll(e: Event) {
		const area = e.target;
		if (!enabled.value || !(area instanceof Element) || area === main.value)
			return;
		slide.scrolled(area, Date.now());
	}

	function onWheel(e: WheelEvent) {
		if (!enabled.value || e.ctrlKey || e.shiftKey) return;
		const used = slide.wheel(
			areaAt(e.target),
			e.deltaY,
			e.deltaMode,
			main.value?.clientHeight ?? 0,
			Date.now(),
		);
		if (used) e.preventDefault();
	}

	const touchY = (e: TouchEvent) =>
		e.touches.length === 1 ? e.touches[0].clientY : null;

	function onTouchStart(e: TouchEvent) {
		slide.touchStart(touchY(e));
	}

	function onTouchMove(e: TouchEvent) {
		if (!enabled.value) return;
		const used = slide.touchMove(areaAt(e.target), touchY(e), Date.now());
		if (used && e.cancelable) e.preventDefault();
	}

	function onTouchEnd() {
		slide.touchEnd();
	}

	// Tabbing to one of the title row's actions brings the row back.
	function onFocusIn(e: Event) {
		if (
			e.target instanceof Element &&
			e.target.closest('[data-testid="settings-title-row"]')
		)
			slide.reset();
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
		for (const [type, fn, opts] of listeners)
			col.addEventListener(type, fn, opts);
	});

	// A new page, or a page without the header, starts with it shown.
	watch(enabled, () => slide.reset());

	onBeforeUnmount(() => {
		const col = main.value;
		if (!col) return;
		for (const [type, fn, opts] of listeners)
			col.removeEventListener(type, fn, { capture: opts.capture });
	});

	return { reset: () => slide.reset(), offset };
}
