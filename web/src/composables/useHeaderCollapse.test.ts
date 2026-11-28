import { describe, expect, it } from "vitest";
import { collapsedCount, planCollapse } from "./useHeaderCollapse";

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

describe("planCollapse by priority (#1584)", () => {
	// The ladder: hide the title (120px plus a 48px spare), shrink the
	// 130px instance badge and the 260px details to 24px icons, then move
	// two 44px buttons and the two icons into "…".
	const savings = [128, 106, 236, 44, 44, 32, 32];
	const refill = [true, true, true, false, false, false, false];
	const spare = [48, 0, 0, 0, 0, 0, 0];
	const group = [
		undefined,
		"instance",
		"info",
		undefined,
		undefined,
		"info",
		"instance",
	];
	const full = 1000;
	const plan = (available: number, taken: boolean[] = []) =>
		planCollapse({
			width: full,
			taken,
			available,
			savings,
			refill,
			spare,
			group,
		});
	const on = (p: boolean[]) => p.flatMap((t, i) => (t ? [i] : []));

	it("shows the title only with room to spare", () => {
		expect(on(plan(full + 48))).toEqual([]);
		// It fits, but not with 48px to spare.
		expect(on(plan(full + 47))).toEqual([0]);
		expect(on(plan(full))).toEqual([0]);
	});

	it("keeps the badge inline once the wider details make room", () => {
		// Without the title, the line is 872px. Shrinking the badge alone
		// isn't enough at 700px, so the details shrink too, freeing 236px,
		// and the badge fits again: 872 - 236 = 636px.
		expect(on(plan(700))).toEqual([0, 2]);
		// At 600px the badge has to go too.
		expect(on(plan(600))).toEqual([0, 1, 2]);
	});

	it("brings the title back when a later step leaves room to spare", () => {
		// A 400px title that is hidden first, then a 500px item that shrinks
		// by 480px: once it has, the title fits again with room to spare.
		const p = planCollapse({
			width: 1000,
			taken: [],
			available: 580,
			savings: [408, 480],
			refill: [true, false],
			spare: [48, 0],
		});
		expect(p).toEqual([false, true]);
	});

	it("moves buttons and then the icons into the menu, one at a time", () => {
		expect(on(plan(530 - 44))).toEqual([0, 1, 2, 3]);
		expect(on(plan(530 - 45))).toEqual([0, 1, 2, 3, 4]);
		expect(on(plan(400))).toEqual([0, 1, 2, 3, 4, 5, 6]);
	});

	it("never grows an icon back into a badge that is in the menu", () => {
		// The instance icon is in the menu, so its badge stays shrunk even
		// with room for the badge's saving.
		const p = planCollapse({
			width: 100,
			taken: [true, true, true, true, true, true, true],
			available: 100 + 32 + 50,
			savings,
			refill,
			group,
		});
		expect(p[6]).toBe(false);
		expect(p[1]).toBe(true);
	});

	it("gives the same plan from any starting state", () => {
		for (const available of [1100, 900, 700, 500, 300]) {
			const fromFull = plan(available);
			const fromCollapsed = planCollapse({
				width: full - savings.reduce((a, b) => a + b, 0),
				taken: savings.map(() => true),
				available,
				savings,
				refill,
				spare,
				group,
			});
			expect(fromCollapsed).toEqual(fromFull);
		}
	});
});
