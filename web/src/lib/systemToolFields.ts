import type { MCPToolInfo } from "../composables/useMCP";

// Pure helpers behind the System > Tools "Run Tool" modal: they turn an MCP
// tool's JSON input schema into form fields, parse the entered values back
// into tool arguments, and remember each tool's last run in sessionStorage.

export interface ToolFieldOption {
	label: string;
	value: string;
}

export interface ToolField {
	name: string;
	kind: "text" | "boolean" | "enum" | "json";
	placeholder: string;
	helpText: string;
	required: boolean;
	enumOptions: ToolFieldOption[];
	schema: Record<string, unknown>;
}

export interface StoredToolRunState {
	values?: Record<string, string>;
	output?: string;
}

export const TOOL_RUN_STORAGE_PREFIX = "aviary:system-tools:runner:";

export function toolSchemaProperties(
	tool: MCPToolInfo,
): Record<string, Record<string, unknown>> {
	const properties = tool.inputSchema?.properties;
	return properties && typeof properties === "object" ? properties : {};
}

// schemaTypes lists a schema's declared types without "null", so an
// optional `["integer", "null"]` reads as an integer.
function schemaTypes(schema: Record<string, unknown>): string[] {
	const rawType = schema.type;
	const types = Array.isArray(rawType) ? rawType : rawType ? [rawType] : [];
	return types.filter(
		(value): value is string => typeof value === "string" && value !== "null",
	);
}

function hasEnum(schema: Record<string, unknown>): schema is {
	enum: unknown[];
} {
	return Array.isArray(schema.enum) && schema.enum.length > 0;
}

function schemaDescription(schema: Record<string, unknown>): string {
	return typeof schema.description === "string"
		? schema.description.trim()
		: "";
}

export function toolFieldKind(
	schema: Record<string, unknown>,
): ToolField["kind"] {
	if (hasEnum(schema)) return "enum";
	const types = schemaTypes(schema);
	if (types.includes("boolean")) return "boolean";
	if (types.includes("object") || types.includes("array")) return "json";
	return "text";
}

export function formatSchemaValue(value: unknown): string {
	if (typeof value === "string") return value;
	try {
		return JSON.stringify(value);
	} catch {
		return String(value);
	}
}

export function toolFieldPlaceholder(
	name: string,
	schema: Record<string, unknown>,
): string {
	if (schema.default !== undefined) {
		return `Default: ${formatSchemaValue(schema.default)}`;
	}
	if (hasEnum(schema)) return `Choose ${name}`;
	const types = schemaTypes(schema);
	if (types.includes("boolean")) return "Select true or false";
	if (types.includes("object") || types.includes("array")) {
		return `Enter ${types.includes("array") ? "a JSON array" : "a JSON object"}`;
	}
	const description = schemaDescription(schema);
	if (description) return description;
	if (types.includes("integer")) return "Enter an integer";
	if (types.includes("number")) return "Enter a number";
	return `Enter ${name}`;
}

export function toolFieldHelpText(schema: Record<string, unknown>): string {
	const parts: string[] = [];
	const description = schemaDescription(schema);
	if (description) parts.push(description);
	const types = schemaTypes(schema);
	if (types.length > 0) parts.push(`Type: ${types.join(" | ")}`);
	return parts.join(" ");
}

export function buildToolFields(tool: MCPToolInfo): ToolField[] {
	const required = new Set(tool.inputSchema?.required ?? []);
	return Object.entries(toolSchemaProperties(tool))
		.sort((a, b) => a[0].localeCompare(b[0]))
		.map(([name, schema]) => ({
			name,
			kind: toolFieldKind(schema),
			placeholder: toolFieldPlaceholder(name, schema),
			helpText: toolFieldHelpText(schema),
			required: required.has(name),
			enumOptions: Array.isArray(schema.enum)
				? schema.enum.map((choice) => ({
						label: formatSchemaValue(choice),
						value: formatSchemaValue(choice),
					}))
				: [],
			schema,
		}));
}

export function initialFieldValue(field: ToolField): string {
	if (field.schema.default === undefined) return "";
	if (field.kind === "json") {
		try {
			return JSON.stringify(field.schema.default, null, 2);
		} catch {
			return "";
		}
	}
	return formatSchemaValue(field.schema.default);
}

export function parseToolFieldValue(field: ToolField, value: string): unknown {
	switch (field.kind) {
		case "boolean":
			if (value === "true") return true;
			if (value === "false") return false;
			throw new Error(`${field.name} must be true or false.`);
		case "enum":
			return value;
		case "json":
			try {
				return JSON.parse(value);
			} catch (error) {
				throw new Error(
					`${field.name} must be valid JSON: ${error instanceof Error ? error.message : String(error)}`,
				);
			}
		case "text": {
			const types = schemaTypes(field.schema);
			if (types.includes("integer")) {
				const parsed = Number(value);
				if (!Number.isInteger(parsed)) {
					throw new Error(`${field.name} must be an integer.`);
				}
				return parsed;
			}
			if (types.includes("number")) {
				const parsed = Number(value);
				if (!Number.isFinite(parsed)) {
					throw new Error(`${field.name} must be a number.`);
				}
				return parsed;
			}
			return value;
		}
	}
}

export function toolRunStorageKey(toolName: string): string {
	return `${TOOL_RUN_STORAGE_PREFIX}${toolName}`;
}

export function readStoredToolRunState(toolName: string): StoredToolRunState {
	if (typeof window === "undefined") return {};
	try {
		const raw = window.sessionStorage.getItem(toolRunStorageKey(toolName));
		if (!raw) return {};
		const parsed = JSON.parse(raw) as StoredToolRunState | null;
		if (!parsed || typeof parsed !== "object") return {};
		return parsed;
	} catch {
		return {};
	}
}

export function writeStoredToolRunState(
	toolName: string,
	state: StoredToolRunState,
): void {
	if (typeof window === "undefined") return;
	try {
		window.sessionStorage.setItem(
			toolRunStorageKey(toolName),
			JSON.stringify(state),
		);
	} catch {
		// Ignore storage failures and keep the modal functional.
	}
}
