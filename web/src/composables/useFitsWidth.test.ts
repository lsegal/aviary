import { expect, it } from "vitest";
import { fitsWidth } from "./useFitsWidth";

// #1584: the CLI providers show inline only when their one-line width fits
// in the board's content box.
it("fits content no wider than the container's content box", () => {
	expect(fitsWidth(600, 648, 48)).toBe(true);
	expect(fitsWidth(600.4, 648, 48)).toBe(true);
	expect(fitsWidth(601, 648, 48)).toBe(false);
});
