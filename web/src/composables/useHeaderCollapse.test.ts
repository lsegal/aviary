import { describe, expect, it } from "vitest";
import { collapsedCount } from "./useHeaderCollapse";

const ICONS = [36, 36];
const SLOT = 36 + 8;

describe("collapsedCount (#1488)", () => {
	it("keeps every button when the row fits and no threshold applies", () => {
		expect(collapsedCount(300, 0, 900, ICONS, [600, 420])).toBe(0);
	});

	it("drops buttons below their width thresholds, Logs first", () => {
		expect(collapsedCount(300, 0, 599, ICONS, [600, 420])).toBe(1);
		expect(collapsedCount(300, 0, 419, ICONS, [600, 420])).toBe(2);
		// A board without Watch has only Logs to drop.
		expect(collapsedCount(300, 0, 300, [36], [600, 420])).toBe(1);
	});

	it("drops as many buttons as the row needs to fit", () => {
		expect(collapsedCount(700, 0, 700 - SLOT, ICONS)).toBe(1);
		expect(collapsedCount(700, 0, 700 - SLOT - 1, ICONS)).toBe(2);
		expect(collapsedCount(700, 0, 100, ICONS)).toBe(2);
	});

	it("is stable once buttons are already in the menu", () => {
		// The row measures narrower with one button moved, but its full width
		// still doesn't fit, so the count stays.
		expect(collapsedCount(700 - SLOT, 1, 700 - SLOT, ICONS)).toBe(1);
		// With room again, the button comes back.
		expect(collapsedCount(700 - SLOT, 1, 700, ICONS)).toBe(0);
	});
});

describe("collapsedCount on a single line (#1576)", () => {
	// Logs, Watch, Chat, Schedule, Queue, then the details and instance
	// icons, which are narrower than a button.
	const widths = [36, 36, 36, 36, 36, 24, 24];
	const thresholds = [600, 420];

	it("moves one item at a time, in order, until the line fits", () => {
		// A row that fits still moves Logs and Watch below their thresholds.
		expect(collapsedCount(300, 0, 500, widths, thresholds)).toBe(1);
		expect(collapsedCount(300, 0, 419, widths, thresholds)).toBe(2);
		// Each pixel short of the next item's slot moves one more.
		const full = 600;
		let prev = 0;
		for (let available = 600; available >= 200; available -= 4) {
			const n = collapsedCount(full, 0, available, widths);
			expect(n).toBeGreaterThanOrEqual(prev);
			prev = n;
		}
		expect(prev).toBe(widths.length);
		expect(collapsedCount(full, 0, full - 44, widths)).toBe(1);
		expect(collapsedCount(full, 0, full - 44 * 5 - 1, widths)).toBe(6);
	});

	it("uses each moved item's own width", () => {
		// The five buttons are gone; the details icon's 32px slot is next.
		const shown = 600 - 44 * 5;
		expect(collapsedCount(shown, 5, shown, widths)).toBe(5);
		expect(collapsedCount(shown, 5, shown - 32, widths)).toBe(6);
		expect(collapsedCount(shown, 5, shown - 33, widths)).toBe(7);
	});

	it("never moves more items than it has", () => {
		expect(collapsedCount(1000, 0, 10, widths)).toBe(widths.length);
		expect(collapsedCount(1000, 0, 10, [])).toBe(0);
	});
});
