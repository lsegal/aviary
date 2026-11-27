// How many of a page header's optional icon buttons move into its "…" menu
// (#1488). The Code Loop board drops Logs first, then Watch, as the header
// narrows, so a phone's row keeps only the buttons that matter most.
//
// The actions sit in SettingsPageHeader's settings-actions row, which never
// wraps and is at most as wide as the title row's content box. A button
// moves into the menu below its width threshold, and also whenever the row
// would not fit otherwise. Every optional button is a HEADER_ICON_CLS square
// (36px) plus the row's 8px gap, so the row's full width is its current
// width plus that much per button already in the menu. Measuring that way
// keeps the result stable as buttons come and go.

import {
	nextTick,
	onBeforeUnmount,
	onMounted,
	type Ref,
	ref,
	type WatchSource,
	watch,
} from "vue";

/** An optional icon button's width plus the action row's gap. */
export const HEADER_OPTIONAL_SLOT_PX = 44;

/**
 * The number of buttons to move into the menu so that a row now `width`
 * wide, with `dropped` buttons already moved, fits in `available`, at most
 * `optional`. The i-th button also moves below `thresholds[i]` pixels.
 */
export function collapsedCount(
	width: number,
	dropped: number,
	available: number,
	optional: number,
	thresholds: readonly number[] = [],
): number {
	const full = width + dropped * HEADER_OPTIONAL_SLOT_PX;
	let count = Math.min(
		optional,
		thresholds.filter((t) => available < t).length,
	);
	while (count < optional && full - count * HEADER_OPTIONAL_SLOT_PX > available)
		count++;
	return count;
}

/**
 * Watches the header row holding `anchor` and returns how many of its
 * `optional()` buttons belong in the menu, the i-th one at least below
 * `thresholds[i]` pixels of row. Outside a header row, such as in
 * HeaderOverflow's popover, nothing collapses. `deps` re-measures when the
 * row's contents change without resizing it.
 */
export function useHeaderCollapse(
	anchor: Ref<HTMLElement | null | undefined>,
	optional: () => number,
	thresholds: readonly number[] = [],
	deps: WatchSource[] = [],
): Ref<number> {
	const collapsed = ref(0);
	let observer: ResizeObserver | null = null;
	let observed: Element[] = [];

	function measure() {
		const row = anchor.value?.closest<HTMLElement>(
			'[data-testid="settings-actions"]',
		);
		const titleRow = row?.parentElement;
		if (!row || !titleRow) {
			collapsed.value = 0;
			return;
		}
		const style = getComputedStyle(titleRow);
		const available =
			titleRow.clientWidth -
			Number.parseFloat(style.paddingLeft) -
			Number.parseFloat(style.paddingRight);
		const max = optional();
		collapsed.value = collapsedCount(
			row.scrollWidth,
			Math.min(collapsed.value, max),
			available,
			max,
			thresholds,
		);
		observe([row, titleRow]);
	}

	function observe(els: Element[]) {
		if (
			!observer ||
			(els.length === observed.length && els.every((e, i) => e === observed[i]))
		)
			return;
		observer.disconnect();
		for (const el of els) observer.observe(el);
		observed = els;
	}

	onMounted(() => {
		if (typeof ResizeObserver !== "undefined")
			observer = new ResizeObserver(() => measure());
		void nextTick(measure);
	});
	onBeforeUnmount(() => observer?.disconnect());
	watch([anchor, optional, ...deps], () => void nextTick(measure), {
		flush: "post",
	});

	return collapsed;
}
