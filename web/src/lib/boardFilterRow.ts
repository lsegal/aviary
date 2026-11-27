import { BOARD_FILTERS, type BoardFilter } from "./projects";

/** The narrowest the board's search box gets on desktop widths (#1490). */
export const SEARCH_MIN = 220;
/** The toolbar's gap between its items (gap-x-3). */
export const ROW_GAP = 12;
/** Below this a toolbar is a phone's: search takes its own row. */
export const STACK_BELOW = 480;

export interface FilterRowLayout {
	/** Search takes its own full-width row. */
	stacked: boolean;
	/** The state filters fold into a "Filters" button. */
	collapsed: boolean;
}

/**
 * Lays out the Code Loop board's toolbar from the width it actually has
 * (#1490). Search keeps at least SEARCH_MIN; the segmented state filters
 * show only when they fit beside it with the items that share the row
 * (`others`, each followed by a gap), and fold otherwise. An unmeasured row
 * (width 0) shows the full control so it can be measured.
 */
export function filterRowLayout(
	rowWidth: number,
	filterWidth: number,
	others: number[],
): FilterRowLayout {
	const stacked = rowWidth > 0 && rowWidth < STACK_BELOW;
	const rest = others.reduce((sum, w) => sum + (w > 0 ? w + ROW_GAP : 0), 0);
	const collapsed =
		stacked || rowWidth < SEARCH_MIN + ROW_GAP + filterWidth + rest;
	return { stacked, collapsed: rowWidth > 0 && collapsed };
}

// A selected segment tints with its state's color; Starting is orange.
const SEGMENT_ON: Record<BoardFilter, string> = {
	scheduled:
		"bg-violet-100 text-violet-800 dark:bg-violet-900/60 dark:text-violet-100",
	starting:
		"bg-orange-100 text-orange-800 dark:bg-orange-900/60 dark:text-orange-100",
	paused:
		"bg-amber-100 text-amber-800 dark:bg-amber-900/60 dark:text-amber-100",
	running: "bg-blue-100 text-blue-800 dark:bg-blue-900/60 dark:text-blue-100",
	done: "bg-green-100 text-green-800 dark:bg-green-900/60 dark:text-green-100",
	failed: "bg-red-100 text-red-800 dark:bg-red-900/60 dark:text-red-100",
};
const SEGMENT_OFF =
	"text-gray-600 hover:bg-gray-50 dark:text-gray-300 dark:hover:bg-gray-800";

/** A segment's colors: tinted when selected, dimmed but live when empty. */
export function segmentTone(
	state: BoardFilter,
	selected: boolean,
	count: number | null,
): string {
	if (selected) return SEGMENT_ON[state];
	return count === 0 ? `opacity-60 ${SEGMENT_OFF}` : SEGMENT_OFF;
}

/** The folded control's label: "Filters", or "Filters · 2" with two on. */
export function filtersLabel(selected: number): string {
	return selected ? `Filters · ${selected}` : "Filters";
}

/** Each board filter's count, in BOARD_FILTERS order. */
export function filterCounts(
	count: (state: BoardFilter) => number | null,
): Record<BoardFilter, number | null> {
	return Object.fromEntries(BOARD_FILTERS.map((s) => [s, count(s)])) as Record<
		BoardFilter,
		number | null
	>;
}
