import { expect, it } from "vitest";
import { isReleaseVersion, releaseNotesURL, shortVersion } from "./version";

it("abbreviates a dev build's commit SHA only", () => {
	expect(shortVersion("1b5ed321be628789bf30cc775f687291e591bead")).toBe(
		"1b5ed32",
	);
	expect(shortVersion("v0.7.66")).toBe("v0.7.66");
});

it("recognizes only release-stamped versions (#1405)", () => {
	expect(isReleaseVersion("v0.7.56")).toBe(true);
	expect(isReleaseVersion("v1.0.0-rc.1")).toBe(true);
	expect(isReleaseVersion("dev")).toBe(false);
	expect(isReleaseVersion("")).toBe(false);
	expect(isReleaseVersion("1b5ed321be628789bf30cc775f687291e591bead")).toBe(
		false,
	);
	expect(isReleaseVersion("0.7.56")).toBe(false);
});

it("links release versions to their release notes only (#1405)", () => {
	expect(releaseNotesURL("v0.7.56")).toBe(
		"https://github.com/lsegal/aviary/releases/tag/v0.7.56",
	);
	expect(releaseNotesURL("dev")).toBe("");
	expect(releaseNotesURL("")).toBe("");
	expect(releaseNotesURL("1b5ed321be628789bf30cc775f687291e591bead")).toBe("");
});
