import { expect, test } from "@playwright/test";
import { undeclaredArguments } from "./helpers/mockMCP";

// The mock server rejects what the real one would, so a page that sends a
// tool an argument its schema lacks fails its e2e test (#617).
test("the mock server rejects arguments a tool does not declare", () => {
	expect(
		undeclaredArguments("project_job_list", {
			targets: ["lsegal/aviary"],
			limit: 12,
		}),
	).toEqual([]);
	expect(
		undeclaredArguments("project_refresh", { targets: ["lsegal/aviary"] }),
	).toEqual(["targets"]);
	expect(
		undeclaredArguments("project_refresh", { target: "a", bogus: 1 }),
	).toEqual(["bogus"]);
});
