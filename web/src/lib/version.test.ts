import { expect, it } from "vitest";
import { shortVersion } from "./version";

it("abbreviates a dev build's commit SHA only", () => {
	expect(shortVersion("1b5ed321be628789bf30cc775f687291e591bead")).toBe(
		"1b5ed32",
	);
	expect(shortVersion("v0.7.66")).toBe("v0.7.66");
});
