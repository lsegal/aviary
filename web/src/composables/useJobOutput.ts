import { nextTick, onMounted, type Ref, ref, watch } from "vue";

// How job output shows on board cards and in the run view: the structured
// transcript ("rich") or the terminal scrollback ("raw"). One choice applies
// to every card and is remembered per browser.

export type JobOutputMode = "rich" | "raw";

const MODE_KEY = "aviary.projects.outputMode";

const mode = ref<JobOutputMode>(
	localStorage.getItem(MODE_KEY) === "raw" ? "raw" : "rich",
);

watch(mode, (value) => localStorage.setItem(MODE_KEY, value));

export function useJobOutputMode() {
	return mode;
}

/** How close to the bottom, in pixels, still counts as following. */
const FOLLOW_SLACK = 24;

/**
 * Keeps el scrolled to the bottom whenever source changes, unless the user
 * has scrolled up. Scrolling back to the bottom follows again.
 */
export function useStickToBottom(
	el: Ref<HTMLElement | null>,
	source: () => unknown,
) {
	const following = ref(true);

	function onScroll() {
		const node = el.value;
		if (!node) return;
		following.value =
			node.scrollHeight - node.scrollTop - node.clientHeight <= FOLLOW_SLACK;
	}

	async function pin() {
		if (!following.value) return;
		await nextTick();
		const node = el.value;
		if (node) node.scrollTop = node.scrollHeight;
	}

	onMounted(pin);
	watch(source, pin, { flush: "post" });
	// A switched view starts at its bottom.
	watch(el, () => {
		following.value = true;
		void pin();
	});

	return { following, onScroll };
}
