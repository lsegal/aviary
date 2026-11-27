import { describe, expect, it } from "vitest";
import { collapsedCount, HEADER_OPTIONAL_SLOT_PX } from "./useHeaderCollapse";

const SLOT = HEADER_OPTIONAL_SLOT_PX;

describe("collapsedCount (#1488)", () => {
	it("keeps every button when the row fits and no threshold applies", () => {
		expect(collapsedCount(300, 0, 900, 2, [600, 420])).toBe(0);
	});

	it("drops buttons below their width thresholds, Logs first", () => {
		expect(collapsedCount(300, 0, 599, 2, [600, 420])).toBe(1);
		expect(collapsedCount(300, 0, 419, 2, [600, 420])).toBe(2);
		// A board without Watch has only Logs to drop.
		expect(collapsedCount(300, 0, 300, 1, [600, 420])).toBe(1);
	});

	it("drops as many buttons as the row needs to fit", () => {
		expect(collapsedCount(700, 0, 700 - SLOT, 2)).toBe(1);
		expect(collapsedCount(700, 0, 700 - SLOT - 1, 2)).toBe(2);
		expect(collapsedCount(700, 0, 100, 2)).toBe(2);
	});

	it("is stable once buttons are already in the menu", () => {
		// The row measures narrower with one button moved, but its full width
		// still doesn't fit, so the count stays.
		expect(collapsedCount(700 - SLOT, 1, 700 - SLOT, 2)).toBe(1);
		// With room again, the button comes back.
		expect(collapsedCount(700 - SLOT, 1, 700, 2)).toBe(0);
	});
});
