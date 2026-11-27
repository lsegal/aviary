import { expect, it } from "vitest";
import { globalProjectMode, projectMode, withMode } from "./projectSettings";
import { projectModeLabel, projectModeOverride } from "./projects";

// #1461: the picker shows the effective mode and stores an override only
// for the mode that differs from the global one.

it("stores no override for the global mode and the other mode as one", () => {
	expect(projectModeOverride("watch", "watch")).toBe("inherit");
	expect(projectModeOverride("manual", "watch")).toBe("manual");
	expect(projectModeOverride("manual", "manual")).toBe("inherit");
	expect(projectModeOverride("watch", "manual")).toBe("watch");
});

it("reads a project's effective mode: its override, else the global one", () => {
	const section = [
		{ target: "o/a" },
		{ target: "o/b", mode: "manual" as const },
	];
	expect(projectMode(section, "o/a", "watch")).toBe("watch");
	expect(projectMode(section, "o/a", "manual")).toBe("manual");
	expect(projectMode(section, "o/b", "watch")).toBe("manual");
	expect(projectMode(undefined, "o/c", "manual")).toBe("manual");
});

it("clears the override when the global mode is picked", () => {
	const global = globalProjectMode({ projects: { mode: "watch" } });
	let section = withMode(
		[{ target: "o/a" }],
		"o/a",
		projectModeOverride("manual", global),
	);
	expect(section).toEqual([{ target: "o/a", mode: "manual" }]);
	section = withMode(section, "o/a", projectModeOverride("watch", global));
	expect(section).toEqual([{ target: "o/a" }]);
	// Without an override, a new global mode flips the project.
	expect(projectMode(section, "o/a", "manual")).toBe("manual");
});

it("labels the menu with the effective mode only", () => {
	expect(projectModeLabel("watch")).toBe("Issue discovery: Watch");
	expect(projectModeLabel("manual")).toBe(
		"Issue discovery: Manual (no issue watching)",
	);
});
