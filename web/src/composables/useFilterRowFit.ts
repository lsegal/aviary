import { computed, onBeforeUnmount, type Ref, ref, watch } from "vue";
import { filterRowLayout } from "../lib/boardFilterRow";

/**
 * Decides the Code Loop board toolbar's layout from the width the toolbar
 * actually has, not the viewport, so an open chat or logs panel counts
 * (#1490). filterRowLayout makes the call.
 */
export function useFilterRowFit(
	row: Ref<HTMLElement | null>,
	filterWidth: Ref<number>,
	others: Ref<HTMLElement | null>[],
) {
	const rowWidth = ref(0);
	const othersWidths = ref<number[]>([]);

	function measure() {
		rowWidth.value = row.value?.clientWidth ?? 0;
		othersWidths.value = others.map((el) => el.value?.offsetWidth ?? 0);
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

	const layout = computed(() =>
		filterRowLayout(rowWidth.value, filterWidth.value, othersWidths.value),
	);
	const stacked = computed(() => layout.value.stacked);
	const collapsed = computed(() => layout.value.collapsed);
	return { stacked, collapsed };
}
