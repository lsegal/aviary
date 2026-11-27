// How many of a page header's optional items move into its "…" menu (#1488).
// The Code Loop board drops Logs first, then Watch, as the header narrows,
// so a phone's row keeps only the buttons that matter most. On a phone its
// compact header is a single line of chips and actions (#1576), so the
// chat, Schedule and Queue buttons follow, then the board details and
// instance icons, one at a time until the line fits.
//
// The actions sit in SettingsPageHeader's settings-actions row, which never
// wraps and is at most as wide as the title row's content box. An item
// moves into the menu below its own width threshold, and also whenever the
// row would not fit otherwise. On a single line, the row's chips count
// toward its width too. Each item's width is measured while it shows and
// remembered while it is in the menu, so the row's full width is its
// current width plus each moved item's width and gap. Measuring that way
// keeps the result stable as items come and go, at any text size (#1093).

import {
	nextTick,
	onBeforeUnmount,
	onMounted,
	type Ref,
	ref,
	type WatchSource,
	watch,
} from "vue";

/** An icon button's width, for an item never measured. */
export const HEADER_ITEM_PX = 36;

/** An optional header item, in the order items move into the menu. */
export interface HeaderCollapseItem {
	/** The data-testid of the item's element in the title row. */
	id: string;
	/** The item also moves into the menu on a row narrower than this. */
	below?: number;
}

/**
 * The number of items to move into the menu so that a row now `width`
 * wide, with `dropped` items already moved, fits in `available`. Each
 * item takes `widths[i]` plus `gap`, and the i-th one also moves below
 * `thresholds[i]` pixels of row; earlier items always move first.
 */
export function collapsedCount(
	width: number,
	dropped: number,
	available: number,
	widths: readonly number[],
	thresholds: readonly (number | undefined)[] = [],
	gap = 8,
): number {
	const slot = (i: number) => widths[i] + gap;
	let full = width;
	for (let i = 0; i < Math.min(dropped, widths.length); i++) full += slot(i);
	let count = 0;
	thresholds.forEach((t, i) => {
		if (t !== undefined && available < t) count = Math.max(count, i + 1);
	});
	count = Math.min(count, widths.length);
	let need = full;
	for (let i = 0; i < count; i++) need -= slot(i);
	// Half a pixel absorbs the rounding of scrollWidth.
	while (count < widths.length && need > available + 0.5) {
		need -= slot(count);
		count++;
	}
	return count;
}

/**
 * Watches the header row holding `anchor` and returns how many of its
 * `items()` belong in the menu. With `singleLine()`, the row's status
 * chips share the line with the actions, so they count toward its width.
 * Outside a header row, such as in HeaderOverflow's popover, nothing
 * collapses. `deps` re-measures when the row's contents change without
 * resizing it.
 */
export function useHeaderCollapse(
	anchor: Ref<HTMLElement | null | undefined>,
	items: () => readonly HeaderCollapseItem[],
	options: { singleLine?: () => boolean; deps?: WatchSource[] } = {},
): Ref<number> {
	const collapsed = ref(0);
	const widths = new Map<string, number>();
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
		const list = items();
		for (const { id } of list) {
			const el = titleRow.querySelector(`[data-testid="${id}"]`);
			if (el) widths.set(id, el.getBoundingClientRect().width);
		}
		const style = getComputedStyle(titleRow);
		const available =
			titleRow.clientWidth -
			Number.parseFloat(style.paddingLeft) -
			Number.parseFloat(style.paddingRight);
		const gap = Number.parseFloat(getComputedStyle(row).columnGap) || 0;
		let width = row.scrollWidth;
		const status = titleRow.querySelector<HTMLElement>(
			'[data-testid="settings-status"]',
		);
		const single = options.singleLine?.() ?? false;
		if (single && status)
			width += status.scrollWidth + (Number.parseFloat(style.columnGap) || 0);
		collapsed.value = collapsedCount(
			width,
			Math.min(collapsed.value, list.length),
			available,
			list.map(({ id }) => widths.get(id) ?? HEADER_ITEM_PX),
			list.map(({ below }) => below),
			gap,
		);
		observe(single && status ? [row, titleRow, status] : [row, titleRow]);
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
	watch(
		[
			anchor,
			() =>
				items()
					.map(({ id }) => id)
					.join(),
			...(options.deps ?? []),
		],
		() => void nextTick(measure),
		{ flush: "post" },
	);

	return collapsed;
}
