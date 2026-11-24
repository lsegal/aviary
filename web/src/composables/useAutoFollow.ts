import { computed, nextTick, onBeforeUnmount, type Ref, ref, watch } from "vue";

/** How close to the bottom, in pixels, still counts as at the bottom. */
export const FOLLOW_THRESHOLD = 8;

export interface AutoFollowOptions {
	/** Counts the view's messages or lines, so the "More below" pill can say
	 * how many arrived while the user was scrolled up. */
	count?: () => number;
}

function prefersReducedMotion(): boolean {
	return (
		window.matchMedia?.("(prefers-reduced-motion: reduce)").matches ?? false
	);
}

// Every chat and live log view scrolls the same way (#1072): it opens at its
// newest line and follows new content while the user is at the bottom.
// Scrolling up stops following and shows the "More below" pill
// (MoreBelowPill.vue); the pill, the user's own sends, or scrolling back
// down follow again. Content changes are watched in the DOM, so streamed
// text, images that finish loading, a resized pane and the on-screen
// keyboard all keep a following view pinned without the view's help.
export function useAutoFollow(
	el: Ref<HTMLElement | null | undefined>,
	options: AutoFollowOptions = {},
) {
	const following = ref(true);
	const overflowing = ref(false);
	// The count when following stopped; later items are new to the user.
	const seen = ref(0);
	// Only a scroll that moves up stops following, so content shrinking or a
	// smooth scroll toward the bottom never does.
	let lastTop = 0;

	const newCount = computed(() =>
		following.value || !options.count
			? 0
			: Math.max(0, options.count() - seen.value),
	);
	const showMore = computed(() => overflowing.value && !following.value);

	function setFollowing(value: boolean) {
		if (following.value === value) return;
		following.value = value;
		if (!value) seen.value = options.count?.() ?? 0;
	}

	function measure(node: HTMLElement) {
		overflowing.value = node.scrollHeight > node.clientHeight + 1;
	}

	function jump(smooth: boolean) {
		const node = el.value;
		if (!node) return;
		const top = node.scrollHeight;
		if (smooth && !prefersReducedMotion() && node.scrollTo) {
			node.scrollTo({ top, behavior: "smooth" });
		} else {
			node.scrollTop = top;
		}
		lastTop = node.scrollTop;
		measure(node);
	}

	/** Re-pins a following view after its content or size changed. */
	function pin() {
		const node = el.value;
		if (!node) return;
		if (following.value) jump(false);
		else measure(node);
	}

	function onScroll() {
		const node = el.value;
		if (!node) return;
		measure(node);
		const distance = node.scrollHeight - node.scrollTop - node.clientHeight;
		if (distance <= FOLLOW_THRESHOLD) setFollowing(true);
		else if (node.scrollTop < lastTop - 1) setFollowing(false);
		lastTop = node.scrollTop;
	}

	/** Goes to the end and follows again: the pill, a switched view, and the
	 * user's own sends. */
	function scrollToBottom({ smooth = true }: { smooth?: boolean } = {}) {
		setFollowing(true);
		jump(smooth);
	}

	/** Shows a deep-linked message or line instead of the bottom. */
	function reveal(target: Element) {
		setFollowing(false);
		target.scrollIntoView({ block: "center" });
		const node = el.value;
		if (node) lastTop = node.scrollTop;
	}

	/** Keeps the visible content still while add puts older history above it. */
	async function keepPosition(add: () => Promise<unknown> | unknown) {
		const before = el.value;
		const fromBottom = before ? before.scrollHeight - before.scrollTop : 0;
		await add();
		await nextTick();
		const node = el.value;
		if (!node || node !== before || following.value) return;
		node.scrollTop = node.scrollHeight - fromBottom;
		lastTop = node.scrollTop;
		measure(node);
	}

	let mutations: MutationObserver | undefined;
	let resizes: ResizeObserver | undefined;
	function disconnect() {
		mutations?.disconnect();
		resizes?.disconnect();
		mutations = undefined;
		resizes = undefined;
	}

	// Each child is watched for size changes too, since an image or code
	// block grows without a DOM mutation.
	function observeChildren(node: HTMLElement) {
		for (const child of node.children) resizes?.observe(child);
	}

	watch(
		el,
		(node) => {
			disconnect();
			if (!node) return;
			following.value = true;
			if (typeof ResizeObserver !== "undefined") {
				resizes = new ResizeObserver(pin);
				resizes.observe(node);
				observeChildren(node);
			}
			mutations = new MutationObserver(() => {
				observeChildren(node);
				pin();
			});
			mutations.observe(node, {
				childList: true,
				subtree: true,
				characterData: true,
			});
			jump(false);
		},
		{ immediate: true, flush: "post" },
	);

	// The on-screen keyboard shrinks the visual viewport without always
	// resizing the view itself.
	window.visualViewport?.addEventListener("resize", pin);
	onBeforeUnmount(() => {
		disconnect();
		window.visualViewport?.removeEventListener("resize", pin);
	});

	return {
		following,
		overflowing,
		showMore,
		newCount,
		onScroll,
		scrollToBottom,
		reveal,
		keepPosition,
	};
}
