import { computed, onBeforeUnmount, onMounted, type Ref, ref } from "vue";

const FADE = "1.5rem";

// A row that scrolls sideways instead of wrapping fades each edge that has
// more items past it, so the row shows it continues. Shared by the Code
// Loop chip row and the narrow-screen main row (#1003).
export function useScrollFade(row: Ref<HTMLElement | null | undefined>) {
	const before = ref(false);
	const after = ref(false);

	function measure() {
		const el = row.value;
		if (!el) return;
		before.value = el.scrollLeft > 1;
		after.value = el.scrollLeft + el.clientWidth < el.scrollWidth - 1;
	}

	const fadeStyle = computed(() => {
		if (!before.value && !after.value) return undefined;
		const start = before.value ? `transparent, #000 ${FADE}` : "#000";
		const end = after.value ? `#000 calc(100% - ${FADE}), transparent` : "#000";
		const mask = `linear-gradient(to right, ${start}, ${end})`;
		return { maskImage: mask, WebkitMaskImage: mask };
	});

	let observer: ResizeObserver | undefined;
	/** Re-measures when the row or any of its items resizes. */
	function observe() {
		observer?.disconnect();
		observer = new ResizeObserver(measure);
		if (row.value) {
			observer.observe(row.value);
			for (const child of row.value.children) observer.observe(child);
		}
		measure();
	}
	onMounted(observe);
	onBeforeUnmount(() => observer?.disconnect());

	/** Scrolls the row, not the page, just enough to show its active item. */
	function revealActive(selector = "[data-active]") {
		const el = row.value;
		const item = el?.querySelector<HTMLElement>(selector);
		if (!el || !item) return;
		const box = item.getBoundingClientRect();
		const left = box.left - el.getBoundingClientRect().left + el.scrollLeft;
		const right = left + box.width;
		if (left < el.scrollLeft) el.scrollLeft = left;
		else if (right > el.scrollLeft + el.clientWidth)
			el.scrollLeft = right - el.clientWidth;
		measure();
	}

	return { before, after, fadeStyle, measure, observe, revealActive };
}
