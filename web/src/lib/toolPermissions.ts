import type { MCPToolInfo } from "../composables/useMCP";

export type PermissionsPreset = "full" | "standard" | "minimal";

export interface ResolveToolPermissionsInput {
	preset?: string;
	availableTools: string[];
	agentTools?: string[];
	agentDisabledTools?: string[];
	overrideRestrictTools?: string[];
	overrideDisabledTools?: string[];
}

export interface ResolvedToolPermissions {
	preset: PermissionsPreset;
	availableTools: string[];
	presetAccessibleTools: string[];
	restrictionSource: "all" | "agent" | "override";
	requestedRestrictTools: string[];
	effectiveRestrictTools: string[];
	disabledSources: {
		agent: string[];
		override: string[];
	};
	effectiveDisabledTools: string[];
	finalTools: string[];
}

export const DEFAULT_PERMISSIONS_PRESET: PermissionsPreset = "standard";

const CATEGORY_LABELS: Record<string, string> = {
	agent: "Agent",
	auth: "Auth",
	browser: "Browser",
	file: "File",
	job: "Jobs",
	memory: "Memory",
	search: "Search",
	server: "Server",
	session: "Sessions",
	skills: "Skills",
	task: "Tasks",
	usage: "Usage",
};

export function normalizePermissionsPreset(preset?: string): PermissionsPreset {
	switch (preset) {
		case "full":
		case "minimal":
		case "standard":
			return preset;
		default:
			return DEFAULT_PERMISSIONS_PRESET;
	}
}

export function toolCategory(name: string): string {
	if (
		name === "ping" ||
		name.startsWith("server_") ||
		name.startsWith("config_")
	) {
		return "server";
	}
	if (name.startsWith("web_")) return "search";
	if (name === "skills_list" || name.startsWith("skill_")) return "skills";
	if (name === "exec" || name.startsWith("exec_")) return "exec";
	if (name.startsWith("file_")) return "file";
	return name.split("_")[0] ?? name;
}

export function toolCategoryLabel(category: string): string {
	return (
		CATEGORY_LABELS[category] ??
		category.charAt(0).toUpperCase() + category.slice(1)
	);
}

export function isToolAccessibleForPreset(
	preset: string | undefined,
	toolName: string,
): boolean {
	const category = toolCategory(toolName);
	switch (normalizePermissionsPreset(preset)) {
		case "full":
			return true;
		case "minimal":
			return ![
				"agent",
				"auth",
				"exec",
				"file",
				"server",
				"browser",
				"skills",
				"usage",
			].includes(category);
		default:
			return !["agent", "auth", "exec", "file", "server"].includes(category);
	}
}

export function isToolGroupAccessibleForPreset(
	preset: string | undefined,
	category: string,
): boolean {
	switch (normalizePermissionsPreset(preset)) {
		case "full":
			return true;
		case "minimal":
			return ![
				"agent",
				"auth",
				"exec",
				"file",
				"server",
				"browser",
				"skills",
				"usage",
			].includes(category);
		default:
			return !["agent", "auth", "exec", "file", "server"].includes(category);
	}
}

export function clampToolNamesForPreset(
	preset: string | undefined,
	names: string[] | undefined,
): string[] {
	if (!names?.length) return [];
	return names.filter((name) => isToolAccessibleForPreset(preset, name));
}

function uniqueToolNames(names: string[] | undefined): string[] {
	if (!names?.length) return [];
	return [...new Set(names)];
}

export function resolveToolPermissions(
	input: ResolveToolPermissionsInput,
): ResolvedToolPermissions {
	const preset = normalizePermissionsPreset(input.preset);
	const availableTools = uniqueToolNames(input.availableTools);
	const presetAccessibleTools = availableTools.filter((name) =>
		isToolAccessibleForPreset(preset, name),
	);

	const requestedRestrictTools = input.overrideRestrictTools?.length
		? uniqueToolNames(input.overrideRestrictTools)
		: uniqueToolNames(input.agentTools);
	const effectiveRestrictTools = clampToolNamesForPreset(
		preset,
		requestedRestrictTools,
	);

	let filteredTools = presetAccessibleTools;
	if (effectiveRestrictTools.length > 0) {
		const allowed = new Set(effectiveRestrictTools);
		filteredTools = presetAccessibleTools.filter((name) => allowed.has(name));
	}

	const agentDisabledTools = clampToolNamesForPreset(
		preset,
		uniqueToolNames(input.agentDisabledTools),
	);
	const overrideDisabledTools = clampToolNamesForPreset(
		preset,
		uniqueToolNames(input.overrideDisabledTools),
	);
	const effectiveDisabledTools = uniqueToolNames([
		...agentDisabledTools,
		...overrideDisabledTools,
	]);

	const blocked = new Set(effectiveDisabledTools);
	const finalTools = filteredTools.filter((name) => !blocked.has(name));

	return {
		preset,
		availableTools,
		presetAccessibleTools,
		restrictionSource: input.overrideRestrictTools?.length
			? "override"
			: input.agentTools?.length
				? "agent"
				: "all",
		requestedRestrictTools,
		effectiveRestrictTools,
		disabledSources: {
			agent: agentDisabledTools,
			override: overrideDisabledTools,
		},
		effectiveDisabledTools,
		finalTools,
	};
}

export function groupTools(tools: MCPToolInfo[]): [string, MCPToolInfo[]][] {
	const groups = new Map<string, MCPToolInfo[]>();
	for (const tool of tools) {
		const category = toolCategory(tool.name);
		const bucket = groups.get(category) ?? [];
		bucket.push(tool);
		groups.set(category, bucket);
	}
	return [...groups.entries()];
}
