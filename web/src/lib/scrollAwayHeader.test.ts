import { describe, expect, it } from "vitest";
import {
	findScrollArea,
	GESTURE_MS,
	HeaderSlide,
	headerTakesGesture,
	scrollToTake,
	slideOffset,
	wheelPixels,
} from "./scrollAwayHeader";

const area = (scrollTop: number, clientHeight = 100, scrollHeight = 500) => ({
	scrollTop,
	clientHeight,
	scrollHeight,
});

describe("slideOffset", () => {
	it("moves the title row within its height", () => {
		expect(slideOffset(0, 30, 120)).toEqual({ next: 30, moved: 30 });
		expect(slideOffset(100, 50, 120)).toEqual({ next: 120, moved: 20 });
		expect(slideOffset(20, -50, 120)).toEqual({ next: 0, moved: -20 });
		expect(slideOffset(120, 10, 120)).toEqual({ next: 120, moved: 0 });
	});
});

describe("wheelPixels", () => {
	it("converts line and page deltas to pixels", () => {
		expect(wheelPixels(40, 0, 700)).toBe(40);
		expect(wheelPixels(3, 1, 700)).toBe(48);
		expect(wheelPixels(-1, 2, 700)).toBe(-700);
	});
});

describe("headerTakesGesture", () => {
	it("hides the row on a page with nothing to scroll", () => {
		expect(headerTakesGesture(30, 0, 120, null)).toBe(true);
		expect(headerTakesGesture(30, 120, 120, null)).toBe(false);
	});

	it("leaves a downward gesture to an area that can still scroll", () => {
		expect(headerTakesGesture(30, 0, 120, area(50))).toBe(false);
		expect(headerTakesGesture(30, 0, 120, area(400))).toBe(true);
	});

	it("brings the row back only past the area's top", () => {
		expect(headerTakesGesture(-30, 120, 120, area(10))).toBe(false);
		expect(headerTakesGesture(-30, 120, 120, area(0))).toBe(true);
		expect(headerTakesGesture(-30, 0, 120, area(0))).toBe(false);
		expect(headerTakesGesture(-30, 60, 120, null)).toBe(true);
	});

	it("ignores a gesture that doesn't move", () => {
		expect(headerTakesGesture(0, 60, 120, null)).toBe(false);
	});
});

describe("scrollToTake", () => {
	it("takes a user's downward scroll until the row is hidden", () => {
		expect(scrollToTake(30, 0, 120, 0)).toBe(30);
		expect(scrollToTake(30, 100, 120, 0)).toBe(20);
		expect(scrollToTake(30, 120, 120, 0)).toBe(0);
	});

	it("leaves upward scrolls and the page's own scrolls alone", () => {
		expect(scrollToTake(-30, 60, 120, 0)).toBe(0);
		expect(scrollToTake(30, 0, 120, GESTURE_MS + 1)).toBe(0);
	});

	it("ignores sub-pixel moves", () => {
		expect(scrollToTake(0.5, 0, 120, 0)).toBe(0);
	});
});

interface Node {
	scrollTop: number;
	clientHeight: number;
	scrollHeight: number;
	parentElement: Node | null;
	overflow: string;
}

const node = (
	parentElement: Node | null,
	overflow = "visible",
	scrollHeight = 100,
): Node => ({
	scrollTop: 0,
	clientHeight: 100,
	scrollHeight,
	parentElement,
	overflow,
});

describe("findScrollArea", () => {
	it("finds the nearest overflowing scroll area below the stop", () => {
		const main = node(null, "hidden", 900);
		const list = node(main, "auto", 800);
		const empty = node(list, "auto");
		const item = node(empty);
		expect(findScrollArea(item, main, (n) => n.overflow)).toBe(list);
		expect(findScrollArea(list, list, (n) => n.overflow)).toBeNull();
		expect(findScrollArea(null, main, (n) => n.overflow)).toBeNull();
	});

	it("skips hidden overflow and never returns the stop", () => {
		const main = node(null, "auto", 900);
		const clip = node(main, "hidden", 800);
		expect(findScrollArea(node(clip), main, (n) => n.overflow)).toBeNull();
		const scroll = node(main, "scroll", 800);
		expect(findScrollArea(scroll, main, (n) => n.overflow)).toBe(scroll);
	});
});

describe("HeaderSlide", () => {
	const setup = () => {
		const seen: number[] = [];
		const slide = new HeaderSlide<ReturnType<typeof area>>(
			() => 120,
			(o) => seen.push(o),
		);
		return { slide, seen };
	};

	it("hands a user's downward scroll to the title row first", () => {
		const { slide, seen } = setup();
		const list = area(0);
		// The wheel marks the gesture and records where the list started.
		expect(slide.wheel(list, 40, 0, 700, 1000)).toBe(false);
		list.scrollTop = 40;
		slide.scrolled(list, 1010);
		expect(slide.offset).toBe(40);
		expect(list.scrollTop).toBe(0);
		expect(seen).toEqual([40]);

		// Past the title row's height the list keeps the rest.
		slide.wheel(list, 100, 0, 700, 1020);
		list.scrollTop = 100;
		slide.scrolled(list, 1030);
		expect(slide.offset).toBe(120);
		expect(list.scrollTop).toBe(20);
	});

	it("leaves the page's own scrolls to the page", () => {
		const { slide } = setup();
		const list = area(0);
		slide.scrolled(list, 0);
		list.scrollTop = 300;
		slide.scrolled(list, 10_000);
		expect(slide.offset).toBe(0);
		expect(list.scrollTop).toBe(300);
	});

	it("brings the row back on a wheel up past the top", () => {
		const { slide } = setup();
		slide.wheel(null, 200, 0, 700, 0);
		expect(slide.offset).toBe(120);
		expect(slide.wheel(area(30), -50, 0, 700, 10)).toBe(false);
		expect(slide.wheel(area(0), -2, 1, 700, 20)).toBe(true);
		expect(slide.offset).toBe(88);
		slide.reset();
		expect(slide.offset).toBe(0);
	});

	it("follows a one-finger drag where nothing else scrolls", () => {
		const { slide } = setup();
		expect(slide.touchMove(null, 300, 0)).toBe(false);
		slide.touchStart(400);
		expect(slide.touchMove(null, 350, 10)).toBe(true);
		expect(slide.offset).toBe(50);
		expect(slide.touchMove(null, 380, 20)).toBe(true);
		expect(slide.offset).toBe(20);
		expect(slide.touchMove(null, null, 30)).toBe(false);
		slide.touchEnd();
		expect(slide.touchMove(null, 300, 40)).toBe(false);
		expect(slide.offset).toBe(20);
	});
});
