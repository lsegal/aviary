import { describe, expect, it } from "vitest";
import {
	nextTaskName,
	replaceRepoReferences,
	repoReferences,
} from "./taskDuplicate";

describe("nextTaskName", () => {
	it("keeps a name that is free in the destination", () => {
		expect(nextTaskName("aviary-release", [])).toBe("aviary-release");
		expect(nextTaskName("aviary-release", ["other"])).toBe("aviary-release");
	});

	it("adds the next free suffix", () => {
		expect(nextTaskName("aviary-release", ["aviary-release"])).toBe(
			"aviary-release-2",
		);
		expect(
			nextTaskName("aviary-release", ["aviary-release", "aviary-release-2"]),
		).toBe("aviary-release-3");
	});

	it("counts up from the highest index taken", () => {
		expect(nextTaskName("x-2", ["x", "x-2"])).toBe("x-3");
		expect(nextTaskName("x", ["x", "x-2", "x-5"])).toBe("x-6");
		expect(nextTaskName("x-7", ["x-7"])).toBe("x-8");
	});
});

describe("repoReferences", () => {
	it("finds the owner/repo name, GitHub URLs and checkout paths", () => {
		const prompt =
			"Release lsegal/aviary: see https://github.com/lsegal/aviary/releases and ~/src/aviary/CHANGELOG.md.";
		expect(repoReferences(prompt, "lsegal/aviary")).toEqual([
			"lsegal/aviary",
			"lsegal/aviary",
			"~/src/aviary/CHANGELOG.md",
		]);
	});

	it("ignores other repositories that share a prefix", () => {
		expect(
			repoReferences("Check lsegal/aviary-docs and aviary", "lsegal/aviary"),
		).toEqual([]);
	});

	it("finds nothing without a source repository", () => {
		expect(repoReferences("lsegal/aviary", "")).toEqual([]);
	});
});

describe("replaceRepoReferences", () => {
	it("replaces every reference with the destination", () => {
		expect(
			replaceRepoReferences(
				"Release LSegal/Aviary from https://github.com/lsegal/aviary.git in /home/me/aviary; leave lsegal/aviary-docs.",
				"lsegal/aviary",
				"lsegal/yard",
			),
		).toBe(
			"Release lsegal/yard from https://github.com/lsegal/yard.git in /home/me/yard; leave lsegal/aviary-docs.",
		);
	});
});
