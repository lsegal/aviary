// Whether an element's one-line width fits inside a container's content box,
// re-measured through a ResizeObserver as either resizes, such as at another
// window width or text size (#1093). The Code Loop board measures a hidden
// copy of its CLI providers this way to show them inline only when they fit
// on one line (#1584).

import { onBeforeUnmount, type Ref, ref, watch } from "vue";

/** Whether `content` wide fits in a `container` box with `padding`. */
export function fitsWidth(
	content: number,
	container: number,
	padding: number,
): boolean {
	// Half a pixel absorbs the rounding of clientWidth.
	return content <= container - padding + 0.5;
}

export function useFitsWidth(
	container: Ref<HTMLElement | null | undefined>,
	content: Ref<HTMLElement | null | undefined>,
): Ref<boolean> {
	const fits = ref(true);
	let observer: ResizeObserver | null = null;

	function measure() {
		const box = container.value;
		const el = content.value;
		if (!box || !el) return;
		const style = getComputedStyle(box);
		fits.value = fitsWidth(
			el.getBoundingClientRect().width,
			box.clientWidth,
			(Number.parseFloat(style.paddingLeft) || 0) +
				(Number.parseFloat(style.paddingRight) || 0),
		);
	}

	watch(
		[container, content],
		([box, el]) => {
			observer?.disconnect();
			observer = null;
			if (!box || !el) return;
			if (typeof ResizeObserver !== "undefined") {
				observer = new ResizeObserver(() => measure());
				observer.observe(box);
				observer.observe(el);
			}
			measure();
		},
		{ flush: "post", immediate: true },
	);
	onBeforeUnmount(() => observer?.disconnect());

	return fits;
}
