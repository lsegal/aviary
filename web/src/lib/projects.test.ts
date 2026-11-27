import { describe, expect, it } from "vitest";
import {
	type CLIStatus,
	cliProblem,
	cliProblemSummary,
	normalizeUpcoming,
	UP_NEXT_WINDOW_MS,
	type UpcomingRun,
	upNextRuns,
} from "./projects";

const run = (task_id: string, at: string) => ({
	task_id,
	name: task_id.split("/")[1],
	at,
});

describe("normalizeUpcoming", () => {
	it("keeps each task's soonest fire only, soonest first (#1574)", () => {
		const runs = normalizeUpcoming([
			run("a/every-30s", "2026-11-27T10:00:30Z"),
			run("a/every-minute", "2026-11-27T10:00:50Z"),
			run("a/every-30s", "2026-11-27T10:01:00Z"),
			run("a/nightly", "2026-11-28T02:00:00Z"),
			run("a/every-minute", "2026-11-27T10:01:50Z"),
			run("a/every-30s", "2026-11-27T10:00:10Z"),
		]);
		expect(runs).toEqual([
			{
				id: "a/every-30s",
				name: "every-30s",
				project: "",
				at: "2026-11-27T10:00:10Z",
			},
			{
				id: "a/every-minute",
				name: "every-minute",
				project: "",
				at: "2026-11-27T10:00:50Z",
			},
			{
				id: "a/nightly",
				name: "nightly",
				project: "",
				at: "2026-11-28T02:00:00Z",
			},
		]);
	});

	it("returns nothing for a payload that is not a list", () => {
		expect(normalizeUpcoming(null)).toEqual([]);
		expect(normalizeUpcoming({})).toEqual([]);
	});
});

describe("upNextRuns", () => {
	const now = Date.parse("2026-11-27T10:00:00Z");
	const due = (id: string, minutes: number): UpcomingRun => ({
		id,
		name: id,
		project: "o/r",
		at: new Date(now + minutes * 60_000).toISOString(),
	});

	it("defaults to a five-minute window (#1577)", () => {
		expect(UP_NEXT_WINDOW_MS).toBe(5 * 60_000);
	});

	it("keeps runs due within the window only (#1577)", () => {
		const runs = [due("soon", 3), due("edge", 5), due("later", 10)];
		expect(upNextRuns(runs, now).map((r) => r.id)).toEqual(["soon", "edge"]);
	});

	it("adds a run as it enters the window (#1577)", () => {
		const runs = [due("later", 10)];
		expect(upNextRuns(runs, now)).toEqual([]);
		expect(upNextRuns(runs, now + 5 * 60_000).map((r) => r.id)).toEqual([
			"later",
		]);
	});

	it("drops a run once its time passes (#1577)", () => {
		const runs = [due("soon", 1)];
		expect(upNextRuns(runs, now + 60_000)).toEqual([]);
		expect(upNextRuns(runs, now + 61_000)).toEqual([]);
	});

	it("takes a custom window", () => {
		expect(upNextRuns([due("later", 10)], now, 15 * 60_000)).toHaveLength(1);
	});
});

const cli = (name: string, quota: string, signedIn = true): CLIStatus => ({
	name,
	models: [],
	quota,
	signedIn,
});

describe("cliProblem (#1580)", () => {
	it("flags signed-out, exhausted and failing providers", () => {
		expect(cliProblem(cli("gemini", "signed out", false))).toBe("signed out");
		expect(cliProblem(cli("claude", "session 0% left, week 40% left"))).toBe(
			"out of quota",
		);
		expect(cliProblem(cli("claude", "0% of 5h quota left"))).toBe(
			"out of quota",
		);
		expect(cliProblem(cli("codex", "quota exhausted"))).toBe("out of quota");
		expect(cliProblem(cli("codex", "unavailable: timed out"))).toBe("error");
	});

	it("leaves healthy and untracked providers alone", () => {
		for (const quota of [
			"week 70% left",
			"10% left",
			"100% left",
			"38% of 5h quota left",
			"quota ok",
			"",
			"not tracked",
		])
			expect(cliProblem(cli("claude", quota)), quota).toBe("");
	});

	it("summarizes every provider with a problem", () => {
		expect(cliProblemSummary([cli("claude", "week 70% left")])).toBe("");
		expect(
			cliProblemSummary([
				cli("claude", "week 0% left"),
				cli("codex", "quota ok"),
				cli("gemini", "signed out", false),
			]),
		).toBe(
			"CLI provider needs attention: claude out of quota, gemini signed out",
		);
	});
});
