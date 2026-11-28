// @vitest-environment happy-dom
import { mount, type VueWrapper } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { type Component, h, nextTick } from "vue";
import SortableList from "./SortableList.vue";

// #1614: SortableList reorders rows by pointer drag, long press, and the
// handle's arrow keys, and emits the new order.

const ROW_HEIGHT = 40;
const GAP = 4;
/** The list's top edge in client coordinates. */
const LIST_TOP = 100;

type Item = { key: string; label: string };
const ITEMS: Item[] = ["a", "b", "c", "d"].map((key) => ({
	key,
	label: key.toUpperCase(),
}));

/** A row's top edge in client coordinates. */
const rowTop = (i: number) => LIST_TOP + i * (ROW_HEIGHT + GAP);

let frames: FrameRequestCallback[] = [];
let reduce = false;
/** The scroll container's box, when a test adds one. */
let scrollBox: { top: number; bottom: number } | null = null;

function rect(top: number, height: number): DOMRect {
	return {
		top,
		bottom: top + height,
		height,
		left: 0,
		right: 200,
		width: 200,
		x: 0,
		y: top,
		toJSON: () => ({}),
	} as DOMRect;
}

beforeEach(() => {
	frames = [];
	reduce = false;
	scrollBox = null;
	vi.stubGlobal("requestAnimationFrame", (cb: FrameRequestCallback) => {
		frames.push(cb);
		return frames.length;
	});
	vi.stubGlobal("cancelAnimationFrame", () => {});
	vi.stubGlobal("matchMedia", (query: string) => ({
		matches: reduce && query.includes("reduce"),
	}));
	vi.spyOn(Element.prototype, "getBoundingClientRect").mockImplementation(
		function (this: Element) {
			if (this.tagName === "OL") return rect(LIST_TOP, 4 * ROW_HEIGHT);
			if (this.tagName === "LI" && this.hasAttribute("data-key")) {
				const rows = [...(this.parentElement?.children ?? [])].filter((el) =>
					el.hasAttribute("data-key"),
				);
				return rect(rowTop(rows.indexOf(this)), ROW_HEIGHT);
			}
			if (scrollBox && (this as HTMLElement).dataset.scroller !== undefined)
				return rect(scrollBox.top, scrollBox.bottom - scrollBox.top);
			return rect(0, 0);
		},
	);
});

afterEach(() => {
	vi.useRealTimers();
	vi.unstubAllGlobals();
	vi.restoreAllMocks();
	document.body.innerHTML = "";
});

/** Runs the queued animation frames once. */
function runFrames() {
	const queued = frames;
	frames = [];
	for (const cb of queued) cb(0);
}

function mountList(props: Record<string, unknown> = {}, attachTo?: Element) {
	return mount(SortableList as unknown as Component, {
		attachTo: attachTo ?? document.body,
		props: { items: ITEMS, ...props },
		slots: {
			default: ({
				item,
				handle,
				dragging,
			}: {
				item: Item;
				handle: Record<string, unknown>;
				dragging: boolean;
			}) =>
				h("div", { "data-dragging-row": dragging ? "true" : undefined }, [
					h("span", { class: "grip", ...handle }, "≡"),
					h("span", { class: "label" }, item.label),
					h("button", { type: "button" }, "edit"),
				]),
		},
	}) as VueWrapper;
}

const row = (w: VueWrapper, key: string) => w.get(`li[data-key="${key}"]`);
const reorders = (w: VueWrapper) =>
	(w.emitted("reorder") ?? []) as [string[], string][];

function pointer(
	type: string,
	init: Partial<PointerEventInit> & { clientY: number },
): PointerEvent {
	return new PointerEvent(type, {
		bubbles: true,
		cancelable: true,
		button: 0,
		pointerType: "mouse",
		clientX: 10,
		...init,
	});
}

function press(target: Element, clientY: number, init = {}) {
	const ev = pointer("pointerdown", { clientY, ...init });
	target.dispatchEvent(ev);
	return ev;
}

const move = (clientY: number, init = {}) =>
	window.dispatchEvent(pointer("pointermove", { clientY, ...init }));

/** Picks a row up by its handle with the mouse and moves it to clientY. */
async function dragTo(w: VueWrapper, key: string, clientY: number) {
	const i = ITEMS.findIndex((item) => item.key === key);
	const grabY = rowTop(i) + ROW_HEIGHT / 2;
	press(row(w, key).get(".grip").element, grabY);
	move(grabY + 10);
	move(clientY);
	await nextTick();
}

describe("rendering", () => {
	it("renders a row per item with the slot and row attributes", () => {
		const w = mountList({
			rowAttrs: (item: Item) => ({ "data-label": item.label }),
		});
		const rows = w.findAll("li[data-key]");
		expect(rows.map((r) => r.attributes("data-key"))).toEqual([
			"a",
			"b",
			"c",
			"d",
		]);
		expect(rows.map((r) => r.attributes("data-label"))).toEqual([
			"A",
			"B",
			"C",
			"D",
		]);
		expect(w.get(".grip").attributes()).toHaveProperty("data-sortable-handle");
		expect(w.find('[data-testid="sortable-placeholder"]').exists()).toBe(false);
		expect(w.get("ol").attributes("data-dragging")).toBeUndefined();
	});
});

describe("mouse drag", () => {
	it("drags a row down and emits the new order after it settles", async () => {
		vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
		const w = mountList();
		// Grab "a" and drop it past "c": its center clears b's and c's.
		await dragTo(w, "a", rowTop(2) + ROW_HEIGHT);
		expect(w.get("ol").attributes("data-dragging")).toBe("true");
		expect(w.find('[data-testid="sortable-placeholder"]').exists()).toBe(true);
		expect(row(w, "a").find('[data-dragging-row="true"]').exists()).toBe(true);
		window.dispatchEvent(pointer("pointerup", { clientY: 0 }));
		await nextTick();
		// The row settles before the order is emitted.
		expect(reorders(w)).toEqual([]);
		vi.advanceTimersByTime(180);
		await nextTick();
		expect(reorders(w)).toEqual([[["b", "c", "a", "d"], "a"]]);
		expect(w.find('[data-testid="sortable-placeholder"]').exists()).toBe(false);
		runFrames();
	});

	it("drags a row up", async () => {
		reduce = true;
		const w = mountList();
		await dragTo(w, "d", rowTop(1) - 10);
		window.dispatchEvent(pointer("pointerup", { clientY: 0 }));
		await nextTick();
		expect(reorders(w)).toEqual([[["a", "d", "b", "c"], "d"]]);
	});

	it("clamps the row to the list and drops it at the end", async () => {
		reduce = true;
		const w = mountList();
		await dragTo(w, "b", 5000);
		window.dispatchEvent(pointer("pointerup", { clientY: 0 }));
		await nextTick();
		expect(reorders(w)).toEqual([[["a", "c", "d", "b"], "b"]]);
	});

	it("emits nothing when the row drops back in its place", async () => {
		reduce = true;
		const w = mountList();
		await dragTo(w, "b", rowTop(1) + ROW_HEIGHT / 2 + 8);
		window.dispatchEvent(pointer("pointerup", { clientY: 0 }));
		await nextTick();
		expect(reorders(w)).toEqual([]);
		expect(w.find('[data-testid="sortable-placeholder"]').exists()).toBe(false);
	});

	it("waits for the pointer to pass the slop before dragging", async () => {
		const w = mountList();
		const down = press(row(w, "a").get(".grip").element, rowTop(0) + 20);
		expect(down.defaultPrevented).toBe(true);
		move(rowTop(0) + 22);
		await nextTick();
		expect(w.get("ol").attributes("data-dragging")).toBeUndefined();
		window.dispatchEvent(pointer("pointerup", { clientY: 0 }));
		move(rowTop(3));
		await nextTick();
		expect(w.get("ol").attributes("data-dragging")).toBeUndefined();
		expect(reorders(w)).toEqual([]);
	});

	it("ignores presses off the handle, other buttons, and a second press", async () => {
		const w = mountList();
		const off = press(row(w, "a").get(".label").element, rowTop(0) + 20);
		expect(off.defaultPrevented).toBe(false);
		press(row(w, "a").get(".grip").element, rowTop(0) + 20, { button: 2 });
		move(rowTop(3));
		await nextTick();
		expect(w.get("ol").attributes("data-dragging")).toBeUndefined();

		await dragTo(w, "a", rowTop(2));
		const again = press(row(w, "c").get(".grip").element, rowTop(2) + 20);
		expect(again.defaultPrevented).toBe(false);
	});

	it("puts the row back when Escape is pressed", async () => {
		reduce = true;
		const w = mountList();
		await dragTo(w, "a", rowTop(3));
		const other = new KeyboardEvent("keydown", { key: "Enter" });
		window.dispatchEvent(other);
		expect(w.get("ol").attributes("data-dragging")).toBe("true");
		const esc = new KeyboardEvent("keydown", {
			key: "Escape",
			cancelable: true,
		});
		window.dispatchEvent(esc);
		await nextTick();
		expect(esc.defaultPrevented).toBe(true);
		expect(reorders(w)).toEqual([]);
		expect(w.get("ol").attributes("data-dragging")).toBeUndefined();
		// The drag's listeners are gone.
		move(rowTop(2));
		window.dispatchEvent(pointer("pointerup", { clientY: 0 }));
		expect(reorders(w)).toEqual([]);
	});

	it("puts the row back when the pointer is cancelled", async () => {
		reduce = true;
		const w = mountList();
		await dragTo(w, "a", rowTop(3));
		window.dispatchEvent(pointer("pointercancel", { clientY: 0 }));
		await nextTick();
		expect(reorders(w)).toEqual([]);
		expect(w.get("ol").attributes("data-dragging")).toBeUndefined();
	});

	it("keeps a touch drag from scrolling the page", async () => {
		const w = mountList();
		await dragTo(w, "a", rowTop(2));
		const touch = new Event("touchmove", { cancelable: true });
		window.dispatchEvent(touch);
		expect(touch.defaultPrevented).toBe(true);
		w.unmount();
	});

	it("ignores pointer moves while the row settles", async () => {
		vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
		const w = mountList();
		await dragTo(w, "a", rowTop(2) + ROW_HEIGHT);
		window.dispatchEvent(pointer("pointerup", { clientY: 0 }));
		move(rowTop(0));
		vi.advanceTimersByTime(180);
		await nextTick();
		expect(reorders(w)).toEqual([[["b", "c", "a", "d"], "a"]]);
	});

	it("emits the order at once when the list unmounts while settling", async () => {
		vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
		const onReorder = vi.fn();
		const w = mountList({ onReorder });
		await dragTo(w, "a", rowTop(2) + ROW_HEIGHT);
		window.dispatchEvent(pointer("pointerup", { clientY: 0 }));
		await nextTick();
		expect(onReorder).not.toHaveBeenCalled();
		w.unmount();
		expect(onReorder).toHaveBeenCalledWith(["b", "c", "a", "d"], "a");
	});
});

describe("touch", () => {
	const touch = { pointerType: "touch" };

	it("picks the row up after a long press anywhere on it", async () => {
		vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
		const vibrate = vi.fn();
		vi.stubGlobal("navigator", { ...navigator, vibrate });
		reduce = true;
		const w = mountList();
		const down = press(
			row(w, "b").get(".label").element,
			rowTop(1) + 20,
			touch,
		);
		// A touch press does not cancel the default, so the page can scroll.
		expect(down.defaultPrevented).toBe(false);
		// The context menu a long press would open stays shut.
		const pendingMenu = new Event("contextmenu", { cancelable: true });
		row(w, "b").element.dispatchEvent(pendingMenu);
		expect(pendingMenu.defaultPrevented).toBe(true);
		move(rowTop(1) + 22, touch);
		vi.advanceTimersByTime(350);
		await nextTick();
		expect(vibrate).toHaveBeenCalledWith(10);
		expect(w.get("ol").attributes("data-dragging")).toBe("true");
		const menu = new Event("contextmenu", { cancelable: true });
		row(w, "b").element.dispatchEvent(menu);
		expect(menu.defaultPrevented).toBe(true);
		move(rowTop(0), touch);
		window.dispatchEvent(pointer("pointerup", { clientY: 0, ...touch }));
		await nextTick();
		expect(reorders(w)).toEqual([[["b", "a", "c", "d"], "b"]]);
	});

	it("treats a touch that moves before the long press as a swipe", async () => {
		vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
		const w = mountList();
		press(row(w, "b").get(".label").element, rowTop(1) + 20, touch);
		move(rowTop(1) + 60, touch);
		vi.advanceTimersByTime(350);
		await nextTick();
		expect(w.get("ol").attributes("data-dragging")).toBeUndefined();
		const menu = new Event("contextmenu", { cancelable: true });
		row(w, "b").element.dispatchEvent(menu);
		expect(menu.defaultPrevented).toBe(false);
	});

	it("does not start on a row's buttons", async () => {
		vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
		const w = mountList();
		press(row(w, "b").get("button").element, rowTop(1) + 20, touch);
		vi.advanceTimersByTime(350);
		await nextTick();
		expect(w.get("ol").attributes("data-dragging")).toBeUndefined();
	});

	it("drops a pending press when the touch lifts", async () => {
		vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
		const w = mountList();
		press(row(w, "b").get(".label").element, rowTop(1) + 20, touch);
		window.dispatchEvent(pointer("pointerup", { clientY: 0, ...touch }));
		vi.advanceTimersByTime(350);
		await nextTick();
		expect(w.get("ol").attributes("data-dragging")).toBeUndefined();
	});
});

describe("keyboard", () => {
	const key = (w: VueWrapper, rowKey: string, k: string) => {
		const ev = new KeyboardEvent("keydown", { key: k, cancelable: true });
		row(w, rowKey).get(".grip").element.dispatchEvent(ev);
		return ev;
	};

	it("moves a row one place with the handle's arrow keys", () => {
		const w = mountList();
		expect(key(w, "b", "ArrowDown").defaultPrevented).toBe(true);
		expect(key(w, "b", "ArrowUp").defaultPrevented).toBe(true);
		expect(reorders(w)).toEqual([
			[["a", "c", "b", "d"], "b"],
			[["b", "a", "c", "d"], "b"],
		]);
	});

	it("does not move past the ends or on other keys", () => {
		const w = mountList();
		key(w, "a", "ArrowUp");
		key(w, "d", "ArrowDown");
		expect(key(w, "b", "Enter").defaultPrevented).toBe(false);
		expect(reorders(w)).toEqual([]);
	});

	it("ignores arrow keys during a drag", async () => {
		const w = mountList();
		await dragTo(w, "a", rowTop(2));
		expect(key(w, "c", "ArrowDown").defaultPrevented).toBe(false);
		expect(reorders(w)).toEqual([]);
		w.unmount();
	});
});

describe("context menu", () => {
	it("is left alone when no drag is pending", () => {
		const w = mountList();
		const menu = new Event("contextmenu", { cancelable: true });
		row(w, "a").element.dispatchEvent(menu);
		expect(menu.defaultPrevented).toBe(false);
	});
});

describe("auto-scroll", () => {
	function mountInScroller() {
		const container = document.createElement("div");
		container.dataset.scroller = "";
		container.style.overflowY = "auto";
		Object.defineProperty(container, "scrollHeight", { value: 1000 });
		Object.defineProperty(container, "clientHeight", { value: 200 });
		document.body.append(container);
		scrollBox = { top: 100, bottom: 300 };
		return { container, w: mountList({}, container) };
	}

	it("scrolls up near the top edge and down near the bottom edge", async () => {
		const { container, w } = mountInScroller();
		container.scrollTop = 100;
		await dragTo(w, "b", 100);
		runFrames();
		// The pointer is a whole edge band deep: the fastest speed.
		expect(container.scrollTop).toBe(86);
		move(300 - 24);
		runFrames();
		// Half the band deep: half speed.
		expect(container.scrollTop).toBe(93);
		move(200);
		runFrames();
		expect(container.scrollTop).toBe(93);
		w.unmount();
	});

	it("skips scrolling when the list has no scroll container", async () => {
		const w = mountList();
		await dragTo(w, "b", 0);
		runFrames();
		expect(frames).toHaveLength(1);
		w.unmount();
	});
});
