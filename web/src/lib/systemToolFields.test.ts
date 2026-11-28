// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from "vitest";
import type { MCPToolInfo } from "../composables/useMCP";
import {
	buildToolFields,
	formatSchemaValue,
	initialFieldValue,
	parseToolFieldValue,
	readStoredToolRunState,
	TOOL_RUN_STORAGE_PREFIX,
	type ToolField,
	toolFieldHelpText,
	toolFieldKind,
	toolFieldPlaceholder,
	toolRunStorageKey,
	toolSchemaProperties,
	writeStoredToolRunState,
} from "./systemToolFields";

// #1615: the System > Tools run modal builds its form from each MCP tool's
// JSON input schema.

const field = (
	kind: ToolField["kind"],
	schema: Record<string, unknown> = {},
	name = "arg",
): ToolField => ({
	name,
	kind,
	placeholder: "",
	helpText: "",
	required: false,
	enumOptions: [],
	schema,
});

const circular = () => {
	const value: Record<string, unknown> = {};
	value.self = value;
	return value;
};

describe("toolSchemaProperties", () => {
	it("returns the declared properties", () => {
		const props = { q: { type: "string" } };
		expect(
			toolSchemaProperties({ name: "t", inputSchema: { properties: props } }),
		).toBe(props);
	});

	it("returns an empty object without a schema or properties", () => {
		expect(toolSchemaProperties({ name: "t" })).toEqual({});
		expect(toolSchemaProperties({ name: "t", inputSchema: {} })).toEqual({});
	});
});

describe("toolFieldKind", () => {
	it.each([
		[{ enum: ["a"], type: "boolean" }, "enum"],
		[{ enum: [], type: "boolean" }, "boolean"],
		[{ type: ["boolean", "null"] }, "boolean"],
		[{ type: "object" }, "json"],
		[{ type: ["array"] }, "json"],
		[{ type: "integer" }, "text"],
		[{ type: ["null"] }, "text"],
		[{}, "text"],
	])("maps %j to %s", (schema, kind) => {
		expect(toolFieldKind(schema)).toBe(kind);
	});
});

describe("formatSchemaValue", () => {
	it("keeps strings and JSON-encodes everything else", () => {
		expect(formatSchemaValue("plain")).toBe("plain");
		expect(formatSchemaValue(3)).toBe("3");
		expect(formatSchemaValue({ a: [1] })).toBe('{"a":[1]}');
	});

	it("falls back to String() when the value cannot be encoded", () => {
		expect(formatSchemaValue(circular())).toBe("[object Object]");
	});
});

describe("toolFieldPlaceholder", () => {
	it.each([
		[{ default: 5, type: "integer" }, "Default: 5"],
		[{ default: "x" }, "Default: x"],
		[{ enum: ["a", "b"] }, "Choose arg"],
		[{ type: "boolean" }, "Select true or false"],
		[{ type: "object" }, "Enter a JSON object"],
		[{ type: ["array", "null"] }, "Enter a JSON array"],
		[{ type: "integer", description: "  How many  " }, "How many"],
		[{ type: "integer", description: "   " }, "Enter an integer"],
		[{ type: "number" }, "Enter a number"],
		[{ type: "string" }, "Enter arg"],
	])("uses %j to produce %s", (schema, placeholder) => {
		expect(toolFieldPlaceholder("arg", schema)).toBe(placeholder);
	});
});

describe("toolFieldHelpText", () => {
	it("joins the trimmed description and the non-null types", () => {
		expect(
			toolFieldHelpText({ description: " Limit ", type: ["integer", "null"] }),
		).toBe("Limit Type: integer");
		expect(toolFieldHelpText({ type: ["string", "number"] })).toBe(
			"Type: string | number",
		);
		expect(toolFieldHelpText({ description: "Only text" })).toBe("Only text");
		expect(toolFieldHelpText({ description: 4 })).toBe("");
	});
});

describe("buildToolFields", () => {
	it("builds sorted fields with kinds, requirements, and enum options", () => {
		const tool: MCPToolInfo = {
			name: "demo",
			inputSchema: {
				required: ["name"],
				properties: {
					name: { type: "string", description: "Who" },
					mode: { enum: ["fast", 2] },
					flag: { type: "boolean" },
				},
			},
		};
		const fields = buildToolFields(tool);
		expect(fields.map((f) => [f.name, f.kind, f.required])).toEqual([
			["flag", "boolean", false],
			["mode", "enum", false],
			["name", "text", true],
		]);
		expect(fields[1].enumOptions).toEqual([
			{ label: "fast", value: "fast" },
			{ label: "2", value: "2" },
		]);
		expect(fields[0].enumOptions).toEqual([]);
		expect(fields[2]).toMatchObject({
			placeholder: "Who",
			helpText: "Who Type: string",
			schema: { type: "string", description: "Who" },
		});
	});

	it("returns no fields for a tool without inputs", () => {
		expect(buildToolFields({ name: "bare" })).toEqual([]);
	});
});

describe("initialFieldValue", () => {
	it("is empty without a default", () => {
		expect(initialFieldValue(field("text"))).toBe("");
	});

	it("pretty-prints JSON defaults", () => {
		expect(initialFieldValue(field("json", { default: { a: 1 } }))).toBe(
			'{\n  "a": 1\n}',
		);
	});

	it("is empty when a JSON default cannot be encoded", () => {
		expect(initialFieldValue(field("json", { default: circular() }))).toBe("");
	});

	it("formats scalar defaults", () => {
		expect(initialFieldValue(field("boolean", { default: false }))).toBe(
			"false",
		);
		expect(initialFieldValue(field("text", { default: "hi" }))).toBe("hi");
	});
});

describe("parseToolFieldValue", () => {
	it("parses booleans and rejects anything else", () => {
		expect(parseToolFieldValue(field("boolean"), "true")).toBe(true);
		expect(parseToolFieldValue(field("boolean"), "false")).toBe(false);
		expect(() => parseToolFieldValue(field("boolean"), "yes")).toThrow(
			"arg must be true or false.",
		);
	});

	it("passes enum values through", () => {
		expect(parseToolFieldValue(field("enum"), "fast")).toBe("fast");
	});

	it("parses JSON and reports invalid input", () => {
		expect(parseToolFieldValue(field("json"), '{"a":[1]}')).toEqual({
			a: [1],
		});
		expect(() => parseToolFieldValue(field("json"), "{")).toThrow(
			/^arg must be valid JSON: /,
		);
	});

	it("reports non-Error JSON failures", () => {
		const spy = vi.spyOn(JSON, "parse").mockImplementationOnce(() => {
			throw "boom";
		});
		expect(() => parseToolFieldValue(field("json"), "1")).toThrow(
			"arg must be valid JSON: boom",
		);
		spy.mockRestore();
	});

	it("parses integers and numbers in text fields", () => {
		const int = field("text", { type: ["integer", "null"] });
		expect(parseToolFieldValue(int, "42")).toBe(42);
		expect(() => parseToolFieldValue(int, "4.5")).toThrow(
			"arg must be an integer.",
		);
		const num = field("text", { type: "number" });
		expect(parseToolFieldValue(num, "4.5")).toBe(4.5);
		expect(() => parseToolFieldValue(num, "abc")).toThrow(
			"arg must be a number.",
		);
	});

	it("keeps plain text as a string", () => {
		expect(parseToolFieldValue(field("text", { type: "string" }), "42")).toBe(
			"42",
		);
		expect(parseToolFieldValue(field("text"), "x")).toBe("x");
	});
});

describe("tool run storage", () => {
	afterEach(() => {
		window.sessionStorage.clear();
		vi.restoreAllMocks();
	});

	it("keys each tool under the runner prefix", () => {
		expect(toolRunStorageKey("files_read")).toBe(
			`${TOOL_RUN_STORAGE_PREFIX}files_read`,
		);
	});

	it("round-trips values and output", () => {
		writeStoredToolRunState("t", { values: { a: "1" }, output: "done" });
		expect(window.sessionStorage.getItem(toolRunStorageKey("t"))).toBe(
			'{"values":{"a":"1"},"output":"done"}',
		);
		expect(readStoredToolRunState("t")).toEqual({
			values: { a: "1" },
			output: "done",
		});
	});

	it("returns an empty state for missing, non-object, or corrupt entries", () => {
		expect(readStoredToolRunState("missing")).toEqual({});
		window.sessionStorage.setItem(toolRunStorageKey("null"), "null");
		expect(readStoredToolRunState("null")).toEqual({});
		window.sessionStorage.setItem(toolRunStorageKey("num"), "3");
		expect(readStoredToolRunState("num")).toEqual({});
		window.sessionStorage.setItem(toolRunStorageKey("bad"), "{");
		expect(readStoredToolRunState("bad")).toEqual({});
	});

	it("ignores storage write failures", () => {
		vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
			throw new Error("quota");
		});
		expect(() => writeStoredToolRunState("t", { output: "x" })).not.toThrow();
	});

	it("does nothing without a window", () => {
		vi.stubGlobal("window", undefined);
		try {
			expect(readStoredToolRunState("t")).toEqual({});
			expect(() => writeStoredToolRunState("t", {})).not.toThrow();
		} finally {
			vi.unstubAllGlobals();
		}
	});
});
