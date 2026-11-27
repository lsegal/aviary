import { onScopeDispose, ref } from "vue";

// The pointer can cross the gap between the icon and the popover.
export const HOVER_CLOSE_MS = 150;

/**
 * The open state of a popover that a click, tap or key opens and pins, and
 * that a passing mouse shows as a tooltip (#1570). Pinned, it stays until
 * dismissed; shown on hover, it closes once the pointer leaves the icon
 * and the popover, and neither takes nor returns focus.
 */
export function useHoverPopover() {
	const open = ref(false);
	// Opened by a click, tap or key rather than a passing mouse.
	const pinned = ref(false);
	const hovering = ref(false);
	// A click on the icon while its tooltip shows pins the tooltip open.
	let pinOnToggle = false;
	let closeTimer: ReturnType<typeof setTimeout> | undefined;

	function clearCloseTimer() {
		if (closeTimer !== undefined) clearTimeout(closeTimer);
		closeTimer = undefined;
	}

	/** The popover asks to open or close: a toggle, Esc or outside tap. */
	function onOpenChange(next: boolean) {
		clearCloseTimer();
		if (!next && pinOnToggle) {
			pinOnToggle = false;
			pinned.value = true;
			return;
		}
		pinOnToggle = false;
		open.value = next;
		pinned.value = next;
		if (next) hovering.value = false;
	}

	function onTriggerPointerDown() {
		pinOnToggle = open.value && !pinned.value;
	}

	/** A pointer inside the popover pins it, such as to edit a field. */
	function pin() {
		pinned.value = true;
	}

	function onHoverStart(e: Pick<PointerEvent, "pointerType">) {
		if (e.pointerType !== "mouse") return;
		clearCloseTimer();
		if (open.value) return;
		hovering.value = true;
		open.value = true;
	}

	function onHoverEnd(e: Pick<PointerEvent, "pointerType">) {
		if (e.pointerType !== "mouse" || pinned.value || !open.value) return;
		clearCloseTimer();
		closeTimer = setTimeout(() => {
			closeTimer = undefined;
			if (!pinned.value) open.value = false;
		}, HOVER_CLOSE_MS);
	}

	// A tooltip shown on hover leaves focus where it was.
	function onOpenAutoFocus(e: Pick<Event, "preventDefault">) {
		if (hovering.value) e.preventDefault();
	}

	function onCloseAutoFocus(e: Pick<Event, "preventDefault">) {
		if (hovering.value) e.preventDefault();
		hovering.value = false;
	}

	onScopeDispose(clearCloseTimer);

	return {
		open,
		pinned,
		onOpenChange,
		onTriggerPointerDown,
		pin,
		onHoverStart,
		onHoverEnd,
		onOpenAutoFocus,
		onCloseAutoFocus,
	};
}
