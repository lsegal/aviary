import { expect, it } from "vitest";
import {
	filterCounts,
	filterRowLayout,
	filtersLabel,
	ROW_GAP,
	SEARCH_MIN,
	STACK_BELOW,
	segmentTone,
} from "./boardFilterRow";

// #1490: the board's state filters show as one segmented control when they
// fit beside a usable search box and fold into "Filters" otherwise.

const FILTER = 470;
const OTHERS = [36, 60];
const NEEDED = SEARCH_MIN + ROW_GAP + FILTER + 36 + ROW_GAP + 60 + ROW_GAP;

it("shows the full control when search, the filters and the rest fit", () => {
	expect(filterRowLayout(NEEDED, FILTER, OTHERS)).toEqual({
		stacked: false,
		collapsed: false,
	});
	expect(filterRowLayout(1200, FILTER, OTHERS).collapsed).toBe(false);
});

it("folds the filters rather than squeezing search below its minimum", () => {
	expect(filterRowLayout(NEEDED - 1, FILTER, OTHERS)).toEqual({
		stacked: false,
		collapsed: true,
	});
});

it("puts search on its own row and folds the filters on a phone", () => {
	expect(filterRowLayout(STACK_BELOW - 1, 0, OTHERS)).toEqual({
		stacked: true,
		collapsed: true,
	});
	expect(filterRowLayout(STACK_BELOW, 0, []).stacked).toBe(false);
});

it("skips unrendered items and shows an unmeasured row in full", () => {
	expect(
		filterRowLayout(SEARCH_MIN + ROW_GAP + FILTER, FILTER, [0]).collapsed,
	).toBe(false);
	expect(filterRowLayout(0, FILTER, OTHERS)).toEqual({
		stacked: false,
		collapsed: false,
	});
});

it("tints selected segments and dims empty unselected ones", () => {
	expect(segmentTone("running", true, 3)).toContain("bg-blue-100");
	expect(segmentTone("scheduled", true, 0)).toContain("bg-violet-100");
	expect(segmentTone("done", false, 0)).toMatch(/^opacity-60 /);
	expect(segmentTone("done", false, 2)).not.toContain("opacity-60");
	expect(segmentTone("failed", false, null)).not.toContain("opacity-60");
});

it("labels the folded control with the active count", () => {
	expect(filtersLabel(0)).toBe("Filters");
	expect(filtersLabel(2)).toBe("Filters · 2");
});

it("collects each filter's count in board order", () => {
	const counts = filterCounts((s) => (s === "scheduled" ? null : s.length));
	expect(Object.keys(counts)).toEqual([
		"scheduled",
		"starting",
		"paused",
		"running",
		"done",
		"failed",
	]);
	expect(counts.scheduled).toBeNull();
	expect(counts.done).toBe(4);
});
