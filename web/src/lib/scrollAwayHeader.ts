// The arithmetic behind the page header whose title row slides away under
// the breadcrumb bar as the page scrolls (#1437). The DOM wiring lives in
// composables/useScrollAwayHeader.ts.

/** How long after a wheel or touch move a scroll still counts as the user's. */
export const GESTURE_MS = 400;

/** The pixels in a wheel event's line mode, as browsers commonly use. */
const LINE_PX = 16;

/** A scroll area's vertical position. */
export interface ScrollState {
	scrollTop: number;
	clientHeight: number;
	scrollHeight: number;
}

/**
 * Slides the title row by delta within [0, range], returning the new
 * offset and how far it actually moved.
 */
export function slideOffset(
	offset: number,
	delta: number,
	range: number,
): { next: number; moved: number } {
	const next = Math.min(range, Math.max(0, offset + delta));
	return { next, moved: next - offset };
}

/** A wheel event's vertical delta in pixels, whatever its delta mode. */
export function wheelPixels(
	deltaY: number,
	deltaMode: number,
	pageHeight: number,
): number {
	if (deltaMode === 1) return deltaY * LINE_PX;
	if (deltaMode === 2) return deltaY * pageHeight;
	return deltaY;
}

const atTop = (area: ScrollState) => area.scrollTop <= 0;
const atBottom = (area: ScrollState) =>
	area.scrollTop + area.clientHeight >= area.scrollHeight - 1;

/**
 * Whether the title row takes a wheel or touch gesture of delta pixels
 * (positive scrolls down) rather than the scroll area under it: up only
 * past the area's top while the row is hidden, down only where the area
 * can't scroll further while the row still shows. Without an area the row
 * takes every gesture it can move for.
 */
export function headerTakesGesture(
	delta: number,
	offset: number,
	range: number,
	area: ScrollState | null,
): boolean {
	if (delta === 0) return false;
	if (delta < 0) return offset > 0 && (!area || atTop(area));
	return offset < range && (!area || atBottom(area));
}

/**
 * How much of a scroll area's downward scroll the title row takes: all of
 * it it can while the user is scrolling, none for a scroll the page made
 * itself.
 */
export function scrollToTake(
	down: number,
	offset: number,
	range: number,
	sinceGesture: number,
): number {
	if (down <= 0 || sinceGesture > GESTURE_MS) return 0;
	const { moved } = slideOffset(offset, down, range);
	return moved >= 1 ? moved : 0;
}

/** An element as findScrollArea walks it: its scroll state and parent. */
export interface ScrollNode extends ScrollState {
	parentElement: ScrollNode | null;
}

/**
 * The nearest element from `from` up to, but not including, `stop` that
 * scrolls vertically and has more content than room, if any.
 */
export function findScrollArea<N extends ScrollNode>(
	from: N | null,
	stop: N | null,
	overflowY: (el: N) => string,
): N | null {
	for (let el = from; el && el !== stop; el = el.parentElement as N | null) {
		if (el.scrollHeight <= el.clientHeight + 1) continue;
		const o = overflowY(el);
		if (o === "auto" || o === "scroll") return el;
	}
	return null;
}

/**
 * The title row's slide for one page: how far it has gone, fed by the page's
 * scroll areas and the user's wheel and touch gestures.
 */
export class HeaderSlide<A extends ScrollState & object> {
	/** How far the title row has slid under the breadcrumb bar, in pixels. */
	offset = 0;
	private readonly last = new WeakMap<A, number>();
	private gestureAt = Number.NEGATIVE_INFINITY;
	private touchY: number | null = null;

	/**
	 * @param range the title row's full height: as far as it can slide.
	 * @param changed called with every new offset.
	 */
	constructor(
		private readonly range: () => number,
		private readonly changed: (offset: number) => void = () => {},
	) {}

	/** Shows the whole title row again. */
	reset() {
		this.offset = 0;
		this.changed(0);
	}

	private slide(delta: number): number {
		const { next, moved } = slideOffset(this.offset, delta, this.range());
		this.offset = next;
		if (moved) this.changed(next);
		return moved;
	}

	/**
	 * Takes what it can of a scroll area's latest downward scroll while the
	 * user is scrolling, giving the area back that much so the content stays
	 * where the user put it while the title row leaves.
	 */
	scrolled(area: A, now: number) {
		const top = area.scrollTop;
		const prev = this.last.get(area) ?? top;
		this.last.set(area, top);
		const take = scrollToTake(
			top - prev,
			this.offset,
			this.range(),
			now - this.gestureAt,
		);
		if (!take) return;
		const moved = this.slide(take);
		area.scrollTop = top - moved;
		this.last.set(area, area.scrollTop);
	}

	/**
	 * Moves the title row for a gesture of delta pixels the page's scroll
	 * area can't use, returning whether it did.
	 */
	private gesture(area: A | null, delta: number): boolean {
		// The area's position before the gesture scrolls it, for scrolled().
		if (area && !this.last.has(area)) this.last.set(area, area.scrollTop);
		if (!headerTakesGesture(delta, this.offset, this.range(), area))
			return false;
		return this.slide(delta) !== 0;
	}

	/** A wheel event over area; true when the title row used it. */
	wheel(
		area: A | null,
		deltaY: number,
		deltaMode: number,
		pageHeight: number,
		now: number,
	): boolean {
		if (deltaY > 0) this.gestureAt = now;
		return this.gesture(area, wheelPixels(deltaY, deltaMode, pageHeight));
	}

	/** A touch starts at y, or with several fingers when y is null. */
	touchStart(y: number | null) {
		this.touchY = y;
	}

	/** The finger moved to y over area; true when the title row used it. */
	touchMove(area: A | null, y: number | null, now: number): boolean {
		this.gestureAt = now;
		if (this.touchY === null || y === null) return false;
		// A finger moving up scrolls the page down.
		const used = this.gesture(area, this.touchY - y);
		this.touchY = y;
		return used;
	}

	touchEnd() {
		this.touchY = null;
	}
}
