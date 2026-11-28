// Which of a page header's optional items move into its "…" menu (#1488).
// The Code Loop board drops Logs first, then Watch, as the header narrows,
// so a phone's row keeps only the buttons that matter most. Its compact
// header is a single line of chips and actions at every width (#1576,
// #1584), filled by priority: the title goes first, then the instance badge
// and the details shrink to icons, then the chat, Schedule and Queue
// buttons move into "…", then the icons, one at a time until the line fits.
// A badge that fits again in the room a wider item left comes back.
//
// The actions sit in SettingsPageHeader's settings-actions row, which never
// wraps and is at most as wide as the title row's content box. An item
// moves into the menu below its own width threshold, and also whenever the
// row would not fit otherwise. On a single line, the row's chips and title
// count toward its width too. Each item's width is measured while it shows
// and remembered while it is in the menu, and an item's wider form is
// measured from a hidden copy, so the line's full width is its current
// width plus what each taken step saved. Measuring that way keeps the
// result stable as items come and go, at any text size (#1093).

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

/** The widths a step's saving is computed from. */
export interface HeaderMeasure {
	/** The width of the element with this data-testid, as last measured. */
	width: (testid: string) => number;
	/** The gap between the status chips. */
	statusGap: number;
	/** The gap between the actions. */
	actionsGap: number;
	/** The gap between the title and the chips. */
	titleGap: number;
}

/**
 * An optional header item, in the order items move into the menu. By
 * default a step moves the item with this data-testid into the menu,
 * saving its width and the actions' gap.
 */
export interface HeaderCollapseItem {
	/** The step's name; by default also the item's data-testid. */
	id: string;
	/** The item also moves into the menu on a row narrower than this. */
	below?: number;
	/**
	 * The data-testids of the elements `saves` reads, measured whenever
	 * they render; by default just `id`.
	 */
	measure?: readonly string[];
	/** The width the step saves, such as a badge's width less its icon's. */
	saves?: (m: HeaderMeasure) => number;
	/**
	 * Undone again after the line fits, when it still fits undone: a
	 * narrower item can come back in the room a wider one left (#1584).
	 */
	refill?: boolean;
	/**
	 * While the step is not taken, the line also needs this much room to
	 * spare, as the title does (#1584).
	 */
	spare?: number;
	/**
	 * Steps sharing a group act on one item, such as shrinking a badge to
	 * an icon and then moving the icon into the menu: a step is not undone
	 * while a later one in its group is taken.
	 */
	group?: string;
}

/** What to fit: each step's saving, threshold, refill and spare room. */
export interface CollapsePlanInput {
	/** The line's width with the steps in `taken` taken. */
	width: number;
	taken: readonly boolean[];
	available: number;
	savings: readonly number[];
	thresholds?: readonly (number | undefined)[];
	refill?: readonly boolean[];
	spare?: readonly number[];
	group?: readonly (string | undefined)[];
}

/**
 * Which steps to take so the line fits in `available`: steps are taken in
 * order until it fits, then any taken step marked `refill` is undone again,
 * the last taken first, when the line still fits without it and no later
 * taken step in its group depends on it. A step is
 * also taken whenever `available` is below its threshold, along with every
 * step before it.
 */
export function planCollapse({
	width,
	taken,
	available,
	savings,
	thresholds = [],
	refill = [],
	spare = [],
	group = [],
}: CollapsePlanInput): boolean[] {
	const n = savings.length;
	let full = width;
	for (let i = 0; i < n; i++) if (taken[i]) full += savings[i];
	let forced = 0;
	thresholds.forEach((t, i) => {
		if (t !== undefined && available < t) forced = Math.max(forced, i + 1);
	});
	forced = Math.min(forced, n);
	const plan = savings.map((_, i) => i < forced);
	const room = () => {
		let need = full;
		for (let i = 0; i < n; i++) need += plan[i] ? -savings[i] : (spare[i] ?? 0);
		return need;
	};
	// Half a pixel absorbs the rounding of scrollWidth.
	let count = forced;
	while (count < n && room() > available + 0.5) plan[count++] = true;
	for (let i = count - 1; i >= forced; i--) {
		if (!refill[i]) continue;
		const g = group[i];
		if (g !== undefined && plan.some((t, j) => t && j > i && group[j] === g))
			continue;
		plan[i] = false;
		if (room() > available + 0.5) plan[i] = true;
	}
	return plan;
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
	return planCollapse({
		width,
		taken: widths.map((_, i) => i < dropped),
		available,
		savings: widths.map((w) => w + gap),
		thresholds,
	}).filter(Boolean).length;
}

/**
 * Watches the header row holding `anchor` and returns the steps among its
 * `items()` to take: the items in the menu, or shown in a narrower form.
 * With `singleLine()`, the row's status chips share the line with the
 * actions, so they count toward its width, and so does the title unless
 * the `title` step hides it. Outside a header row, such as in
 * HeaderOverflow's popover, nothing collapses. `deps` re-measures when the
 * row's contents change without resizing it.
 */
export function useHeaderCollapse(
	anchor: Ref<HTMLElement | null | undefined>,
	items: () => readonly HeaderCollapseItem[],
	options: {
		singleLine?: () => boolean;
		/** The step that hides the title on a single line. */
		title?: string;
		deps?: WatchSource[];
	} = {},
): Ref<ReadonlySet<string>> {
	const taken = ref<ReadonlySet<string>>(new Set());
	const widths = new Map<string, number>();
	let observer: ResizeObserver | null = null;
	let observed: Element[] = [];

	function measure() {
		const row = anchor.value?.closest<HTMLElement>(
			'[data-testid="settings-actions"]',
		);
		const titleRow = row?.parentElement;
		if (!row || !titleRow) {
			if (taken.value.size) taken.value = new Set();
			return;
		}
		const list = items();
		const ids = new Set<string>();
		// Hidden copies measured for a step's saving resize on their own, such
		// as when their text changes, so they are watched too.
		const copies = new Set<string>();
		for (const item of list)
			for (const id of item.measure ?? [item.id]) {
				ids.add(id);
				if (item.measure) copies.add(id);
			}
		const watched: Element[] = [row, titleRow];
		for (const id of ids) {
			const el = titleRow.querySelector<HTMLElement>(`[data-testid="${id}"]`);
			if (!el) continue;
			// A visually hidden title is 1px wide but scrolls its full width.
			widths.set(
				id,
				Math.max(el.getBoundingClientRect().width, el.scrollWidth ?? 0),
			);
			if (copies.has(id)) watched.push(el);
		}
		const px = (v: string | undefined) => Number.parseFloat(v ?? "") || 0;
		const style = getComputedStyle(titleRow);
		const available =
			titleRow.clientWidth - px(style.paddingLeft) - px(style.paddingRight);
		const actionsGap = px(getComputedStyle(row).columnGap);
		let width = row.scrollWidth;
		const status = titleRow.querySelector<HTMLElement>(
			'[data-testid="settings-status"]',
		);
		const statusGap = status ? px(getComputedStyle(status).columnGap) : 0;
		const single = options.singleLine?.() ?? false;
		const titleEl = titleRow.querySelector<HTMLElement>(
			'[data-testid="settings-title"]',
		);
		if (single && status) {
			width += status.scrollWidth + px(style.columnGap);
			watched.push(status);
			if (titleEl && options.title && !taken.value.has(options.title))
				width += titleEl.scrollWidth + titleGap(titleEl);
		}
		const m: HeaderMeasure = {
			width: (id) => widths.get(id) ?? HEADER_ITEM_PX,
			statusGap,
			actionsGap,
			titleGap: titleEl ? titleGap(titleEl) : 0,
		};
		const plan = planCollapse({
			width,
			taken: list.map(({ id }) => taken.value.has(id)),
			available,
			savings: list.map((i) =>
				i.saves ? i.saves(m) : m.width(i.id) + actionsGap,
			),
			thresholds: list.map(({ below }) => below),
			refill: list.map(({ refill }) => !!refill),
			spare: list.map(({ spare }) => spare ?? 0),
			group: list.map(({ group }) => group),
		});
		const next = new Set(list.filter((_, i) => plan[i]).map(({ id }) => id));
		if (
			next.size !== taken.value.size ||
			[...next].some((id) => !taken.value.has(id))
		)
			taken.value = next;
		observe(watched);
	}

	// The gap between the title and the chips beside it.
	function titleGap(title: HTMLElement): number {
		const parent = title.parentElement;
		return parent
			? Number.parseFloat(getComputedStyle(parent).columnGap) || 0
			: 0;
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
			() => options.singleLine?.(),
			...(options.deps ?? []),
		],
		() => void nextTick(measure),
		{ flush: "post" },
	);

	return taken;
}
