import { expect, it } from "vitest";
import { issueSummary } from "./projects";

const targets = [
	{ name: "lsegal/aviary", issues: 1 },
	{ name: "lsegal/forgo", issues: 2 },
	{ name: "lsegal/yard", issues: 0, manual: true },
	{ name: "lsegal/zvid", issues: 0, loading: true },
];

it("totals the board's watched projects and lists each in the tooltip (#1434)", () => {
	expect(
		issueSummary(targets, [
			"lsegal/aviary",
			"lsegal/forgo",
			"lsegal/yard",
			"lsegal/zvid",
		]),
	).toEqual({
		label: "3 open issues",
		title: "lsegal/aviary: 1\nlsegal/forgo: 2\nlsegal/yard: manual",
	});
});

it("counts only the projects on the board", () => {
	expect(issueSummary(targets, ["LSEGAL/Aviary"])).toEqual({
		label: "1 open issue",
		title: "lsegal/aviary: 1",
	});
});

it("has no chip when nothing on the board is counted", () => {
	expect(issueSummary(targets, ["lsegal/yard", "lsegal/zvid"])).toBeNull();
	expect(issueSummary([], ["lsegal/aviary"])).toBeNull();
});
