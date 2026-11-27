import { describe, expect, it } from "vitest";
import {
	GESTURE_MS,
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
