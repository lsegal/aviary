import { computed, onBeforeUnmount, type Ref, ref, watch } from "vue";

/** The narrowest the board's search box gets on desktop widths (#1490). */
export const SEARCH_MIN = 220;
// The toolbar's gap between its items (gap-x-3).
const GAP = 12;
// Below this a toolbar is a phone's: search takes its own row.
const STACK_BELOW = 480;

/**
 * Decides the Code Loop board toolbar's layout from the width the toolbar
 * actually has, not the viewport, so an open chat or logs panel counts
 * (#1490). Search keeps at least SEARCH_MIN; the state filters show in full
 * only when they fit beside it with the items that share the row, and fold
 * into a "Filters" button otherwise.
 */
export function useFilterRowFit(
	row: Ref<HTMLElement | null>,
	filterWidth: Ref<number>,
	others: Ref<HTMLElement | null>[],
) {
	const rowWidth = ref(0);
	const othersWidth = ref(0);

	function measure() {
		rowWidth.value = row.value?.clientWidth ?? 0;
		othersWidth.value = others.reduce(
			(sum, el) => sum + (el.value ? el.value.offsetWidth + GAP : 0),
			0,
		);
	}

	let observer: ResizeObserver | undefined;
	watch(
		[row, ...others],
		(els) => {
			observer?.disconnect();
			observer = new ResizeObserver(measure);
			for (const el of els) if (el) observer.observe(el);
			measure();
		},
		{ immediate: true, flush: "post" },
	);
	onBeforeUnmount(() => observer?.disconnect());

	const stacked = computed(
		() => rowWidth.value > 0 && rowWidth.value < STACK_BELOW,
	);
	const collapsed = computed(
		() =>
			stacked.value ||
			rowWidth.value < SEARCH_MIN + GAP + filterWidth.value + othersWidth.value,
	);
	return { stacked, collapsed };
}
