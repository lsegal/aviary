import { describe, expect, it } from "vitest";
import {
	codeLoopTarget,
	codeLoopTargetLabel,
	isCodeLoopTarget,
	parseCodeLoopTarget,
} from "./taskTargets";

describe("codeLoopTarget", () => {
	it("names the job with its options", () => {
		expect(codeLoopTarget(" owner/repo ", 12)).toBe("codeloop:owner/repo#12");
		expect(
			codeLoopTarget("owner/repo", 12, { resume: true, reopen: true }),
		).toBe("codeloop:owner/repo#12?resume,reopen");
	});

	it("round-trips through parseCodeLoopTarget", () => {
		const target = codeLoopTarget("owner/repo", 7, { reopen: true });
		expect(parseCodeLoopTarget(target)).toEqual({
			project: "owner/repo",
			number: 7,
			reopen: true,
		});
	});
});

describe("parseCodeLoopTarget", () => {
	it("rejects malformed targets", () => {
		for (const target of [
			undefined,
			"notify",
			"codeloop:owner/repo",
			"codeloop:#3",
			"codeloop:owner/repo#0",
			"codeloop:owner/repo#1.5",
			"codeloop:owner/repo#3?bogus",
		]) {
			expect(parseCodeLoopTarget(target)).toBeNull();
		}
	});

	it("still recognizes a malformed target as a Code Loop target", () => {
		expect(isCodeLoopTarget(" codeloop:owner/repo")).toBe(true);
		expect(isCodeLoopTarget("notify")).toBe(false);
	});

	it("labels the job in prose", () => {
		expect(codeLoopTargetLabel({ project: "owner/repo", number: 4 })).toBe(
			"Code Loop owner/repo#4",
		);
	});
});
