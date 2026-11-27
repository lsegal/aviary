import { describe, expect, it } from "vitest";
import { normalizeUpcoming } from "./projects";

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
			{ id: "a/every-30s", name: "every-30s", at: "2026-11-27T10:00:10Z" },
			{
				id: "a/every-minute",
				name: "every-minute",
				at: "2026-11-27T10:00:50Z",
			},
			{ id: "a/nightly", name: "nightly", at: "2026-11-28T02:00:00Z" },
		]);
	});

	it("returns nothing for a payload that is not a list", () => {
		expect(normalizeUpcoming(null)).toEqual([]);
		expect(normalizeUpcoming({})).toEqual([]);
	});
});
