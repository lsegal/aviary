import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { effectScope } from "vue";
import { HOVER_CLOSE_MS, useHoverPopover } from "./useHoverPopover";

// #1570: the Code Loop header's tag and info popovers open pinned on a
// click, tap or key, and as a tooltip on mouse hover.

const mouse = { pointerType: "mouse" };
const touch = { pointerType: "touch" };
const focusEvent = () => ({ preventDefault: vi.fn() });

let scope: ReturnType<typeof effectScope>;
let p: ReturnType<typeof useHoverPopover>;

beforeEach(() => {
	vi.useFakeTimers();
	scope = effectScope();
	p = scope.run(() => useHoverPopover()) as ReturnType<typeof useHoverPopover>;
});

afterEach(() => {
	scope.stop();
	vi.useRealTimers();
});

it("a click opens the popover pinned, moves focus, and Esc closes it", () => {
	p.onTriggerPointerDown();
	p.onOpenChange(true);
	expect(p.open.value).toBe(true);
	expect(p.pinned.value).toBe(true);
	const opened = focusEvent();
	p.onOpenAutoFocus(opened);
	expect(opened.preventDefault).not.toHaveBeenCalled();
	// Leaving with the mouse keeps a pinned popover open.
	p.onHoverEnd(mouse);
	vi.advanceTimersByTime(HOVER_CLOSE_MS * 2);
	expect(p.open.value).toBe(true);
	p.onOpenChange(false);
	expect(p.open.value).toBe(false);
	const closed = focusEvent();
	p.onCloseAutoFocus(closed);
	expect(closed.preventDefault).not.toHaveBeenCalled();
});

it("hover shows a tooltip that leaves focus alone and closes after the pointer leaves", () => {
	p.onHoverStart(mouse);
	expect(p.open.value).toBe(true);
	expect(p.pinned.value).toBe(false);
	const opened = focusEvent();
	p.onOpenAutoFocus(opened);
	expect(opened.preventDefault).toHaveBeenCalled();
	p.onHoverEnd(mouse);
	vi.advanceTimersByTime(HOVER_CLOSE_MS - 1);
	expect(p.open.value).toBe(true);
	vi.advanceTimersByTime(1);
	expect(p.open.value).toBe(false);
	const closed = focusEvent();
	p.onCloseAutoFocus(closed);
	expect(closed.preventDefault).toHaveBeenCalled();
});

it("crossing from the icon into the popover keeps the tooltip open", () => {
	p.onHoverStart(mouse);
	p.onHoverEnd(mouse);
	p.onHoverStart(mouse);
	vi.advanceTimersByTime(HOVER_CLOSE_MS * 2);
	expect(p.open.value).toBe(true);
});

it("clicking the icon while its tooltip shows pins it instead of closing it", () => {
	p.onHoverStart(mouse);
	p.onTriggerPointerDown();
	p.onOpenChange(false);
	expect(p.open.value).toBe(true);
	expect(p.pinned.value).toBe(true);
	p.onHoverEnd(mouse);
	vi.advanceTimersByTime(HOVER_CLOSE_MS * 2);
	expect(p.open.value).toBe(true);
	// A later toggle closes it as usual.
	p.onTriggerPointerDown();
	p.onOpenChange(false);
	expect(p.open.value).toBe(false);
});

it("a pointer inside the tooltip pins it, such as to edit the filter", () => {
	p.onHoverStart(mouse);
	p.pin();
	p.onHoverEnd(mouse);
	vi.advanceTimersByTime(HOVER_CLOSE_MS * 2);
	expect(p.open.value).toBe(true);
});

it("touch pointers never open or close it by hover", () => {
	p.onHoverStart(touch);
	expect(p.open.value).toBe(false);
	p.onOpenChange(true);
	p.onHoverEnd(touch);
	vi.advanceTimersByTime(HOVER_CLOSE_MS * 2);
	expect(p.open.value).toBe(true);
});
