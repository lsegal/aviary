import { describe, expect, it } from "vitest";
import {
	isToolAccessibleForPreset,
	normalizePermissionsPreset,
	toolCategory,
	toolCategoryLabel,
} from "./toolPermissions";

describe("toolCategory", () => {
	it("groups tools by prefix", () => {
		expect(toolCategory("ping")).toBe("server");
		expect(toolCategory("config_get")).toBe("server");
		expect(toolCategory("web_search")).toBe("search");
		expect(toolCategory("skills_list")).toBe("skills");
		expect(toolCategory("exec")).toBe("exec");
		expect(toolCategory("file_read")).toBe("file");
		expect(toolCategory("browser_open")).toBe("browser");
	});

	it("labels unknown categories by capitalizing them", () => {
		expect(toolCategoryLabel("usage")).toBe("Usage");
		expect(toolCategoryLabel("widget")).toBe("Widget");
	});
});

describe("isToolAccessibleForPreset", () => {
	it("falls back to the default preset", () => {
		expect(normalizePermissionsPreset("bogus")).toBe(
			normalizePermissionsPreset(undefined),
		);
	});

	it("narrows access from full to minimal", () => {
		expect(isToolAccessibleForPreset("full", "exec")).toBe(true);
		expect(isToolAccessibleForPreset("standard", "exec")).toBe(false);
		expect(isToolAccessibleForPreset("standard", "browser_open")).toBe(true);
		expect(isToolAccessibleForPreset("minimal", "browser_open")).toBe(false);
		expect(isToolAccessibleForPreset("minimal", "web_search")).toBe(true);
	});
});
