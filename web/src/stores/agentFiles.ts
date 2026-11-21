import { defineStore } from "pinia";
import { ref } from "vue";
import { useMCP } from "../composables/useMCP";

/** Built-in files every agent keeps; they cannot be renamed or deleted. */
export const PROTECTED_AGENT_FILES = [
	"AGENTS.md",
	"SYSTEM.md",
	"MEMORY.md",
	"RULES.md",
] as const;

const protectedFiles = new Set(
	PROTECTED_AGENT_FILES.map((file) => file.toUpperCase()),
);

export function isProtectedAgentFile(file: string): boolean {
	return protectedFiles.has(file.toUpperCase());
}

/** A new file's name: trimmed, with .md added when missing. */
export function normalizeAgentFileName(file: string): string {
	const trimmed = file.trim();
	if (!trimmed) return "";
	return trimmed.toLowerCase().endsWith(".md") ? trimmed : `${trimmed}.md`;
}

/** The Files page of an agent, optionally opened on one file. */
export function agentFilePath(agent: string, file = "") {
	const path = `/agents/${encodeURIComponent(agent)}/files`;
	return file ? { path, query: { file } } : { path };
}

function parseList(raw: string): string[] {
	try {
		const parsed = JSON.parse(raw) as unknown;
		return Array.isArray(parsed) ? parsed.map(String) : [];
	} catch {
		return [];
	}
}

/**
 * Each agent's root markdown files, shared by the sidebar's Files list and
 * the Files page's editor.
 */
export const useAgentFilesStore = defineStore("agentFiles", () => {
	const { callTool } = useMCP();
	const byAgent = ref<Record<string, string[]>>({});
	const synced = new Set<string>();

	function files(agent: string): string[] {
		return byAgent.value[agent] ?? [];
	}

	async function list(agent: string): Promise<string[]> {
		const raw = await callTool("agent_file_list", { agent });
		return parseList(raw).filter((f) => !f.includes("/"));
	}

	/** Lists the agent's files, syncing its templates once when it has none. */
	async function load(agent: string): Promise<string[]> {
		if (!agent) return [];
		let listed = await list(agent);
		if (!listed.length && !synced.has(agent)) {
			synced.add(agent);
			await callTool("agent_template_sync", { agent });
			listed = await list(agent);
		}
		byAgent.value = { ...byAgent.value, [agent]: listed };
		return listed;
	}

	async function syncTemplates(agent: string): Promise<string[]> {
		synced.add(agent);
		await callTool("agent_template_sync", { agent });
		return load(agent);
	}

	/** Creates an empty file and returns its normalized name. */
	async function create(agent: string, name: string): Promise<string> {
		const file = normalizeAgentFileName(name);
		if (!file) throw new Error("File name is required.");
		if (file.includes("/")) throw new Error("Files live at the agent's root.");
		if (files(agent).some((f) => f.toUpperCase() === file.toUpperCase()))
			throw new Error(`${file} already exists.`);
		await callTool("agent_file_write", { agent, file, content: "" });
		await load(agent);
		return file;
	}

	/** Renames a file, keeping its content, and returns the new name. */
	async function rename(
		agent: string,
		from: string,
		name: string,
	): Promise<string> {
		const file = normalizeAgentFileName(name);
		if (!file) throw new Error("File name is required.");
		if (file === from) return file;
		if (file.includes("/")) throw new Error("Files live at the agent's root.");
		if (isProtectedAgentFile(from) || isProtectedAgentFile(file))
			throw new Error("Built-in files cannot be renamed.");
		if (files(agent).some((f) => f.toUpperCase() === file.toUpperCase()))
			throw new Error(`${file} already exists.`);
		const content = await callTool("agent_file_read", { agent, file: from });
		await callTool("agent_file_write", { agent, file, content });
		await callTool("agent_file_delete", { agent, file: from });
		await load(agent);
		return file;
	}

	async function remove(agent: string, file: string): Promise<void> {
		if (isProtectedAgentFile(file))
			throw new Error("Built-in files cannot be deleted.");
		await callTool("agent_file_delete", { agent, file });
		await load(agent);
	}

	return { files, load, syncTemplates, create, rename, remove };
});
