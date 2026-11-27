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
