import { expect, it } from "vitest";
import { queueOptions } from "./queueOptions";

// #1095, #1576: the Queue menu and the "…" menu share the Queue choices.

it("lists fix, safe merge and discuss, with safe merge off under no_merge", () => {
	expect(queueOptions().map((o) => o.skill)).toEqual([
		"gh-fix",
		"gh-safe-merge",
		"gh-discuss",
	]);
	expect(
		queueOptions(false).find((o) => o.skill === "gh-safe-merge")?.disabled,
	).toBe(false);
	const off = queueOptions(true).find((o) => o.skill === "gh-safe-merge");
	expect(off?.disabled).toBe(true);
	expect(off?.title).toMatch(/no_merge/);
});
