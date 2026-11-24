import { type AgentTask, precompilesBesidePrompt } from "../stores/settings";

// Where a precompiling task's compiled script stands, as the compiled status
// chip and the compiled script panel show it, in the task and workflow
// editors, the task page header, and task and workflow cards.

/** One attempt to compile a task's prompt, from task_compile_query. */
export interface TaskCompileAttempt {
	id: string;
	status: "succeeded" | "skipped" | "failed" | string;
	created_at: string;
	prompt?: string;
	reason?: string;
	script?: string;
}

export type CompiledState =
	| "compiled"
	| "stale"
	| "not-compiled"
	| "failed"
	| "fallback"
	| "compiling"
	| "";

export interface CompiledStatus {
	state: CompiledState;
	label: string;
	title: string;
	tone: "green" | "amber" | "red" | "gray";
}

export interface CompiledStatusInput {
	/** The prompt as edited, when an editor shows the status. */
	editedPrompt?: string;
	/** The task's compile attempts, newest first. */
	attempts?: TaskCompileAttempt[];
}

/** The newest compile attempt that produced the script the task holds. */
export function sourceAttempt(
	task: AgentTask,
	attempts: TaskCompileAttempt[] = [],
): TaskCompileAttempt | undefined {
	const script = task.compiled?.trim();
	if (!script) return undefined;
	return (
		attempts.find(
			(a) => a.status === "succeeded" && a.script?.trim() === script,
		) ?? attempts.find((a) => a.status === "succeeded")
	);
}

/**
 * Whether the task's compiled script no longer matches its prompt: the prompt
 * as edited differs from the one the script was compiled from.
 */
export function isStale(
	task: AgentTask,
	{ editedPrompt, attempts }: CompiledStatusInput = {},
): boolean {
	if (!task.compiled?.trim()) return false;
	const prompt = (editedPrompt ?? task.prompt ?? "").trim();
	const source = sourceAttempt(task, attempts)?.prompt;
	return prompt !== (source ?? task.prompt ?? "").trim();
}

/** The short version a compiled script is known by: its compile key's head. */
export function compiledVersion(task: AgentTask): string {
	return (task.compiled_key ?? "").slice(0, 7);
}

export function compiledStatus(
	task: AgentTask,
	input: CompiledStatusInput = {},
): CompiledStatus | null {
	if (!precompilesBesidePrompt(task)) return null;
	const failures = task.script_failures ?? 0;
	const last = input.attempts?.[0];
	if (task.precompile !== true) {
		return task.precompile_note
			? {
					state: "fallback",
					label: "Falling back to model",
					title: task.precompile_note,
					tone: "amber",
				}
			: null;
	}
	if (task.compiled?.trim()) {
		if (isStale(task, input))
			return {
				state: "stale",
				label: "Compiled (stale: prompt changed)",
				title:
					"The prompt changed since its script was compiled; saving compiles it again.",
				tone: "amber",
			};
		if (failures)
			return {
				state: "fallback",
				label: "Falling back to model",
				title: `The compiled script failed its last ${failures === 1 ? "run" : `${failures} runs`}, which fell back to the model.`,
				tone: "amber",
			};
		return {
			state: "compiled",
			label: "Compiled",
			title:
				"Runs execute the compiled script with no model calls, and fall back to the model if it fails.",
			tone: "green",
		};
	}
	if (task.precompile_note)
		return {
			state: "fallback",
			label: "Falling back to model",
			title: task.precompile_note,
			tone: "amber",
		};
	if (last?.status === "failed")
		return {
			state: "failed",
			label: "Compile failed",
			title: last.reason || "The last compile attempt failed.",
			tone: "red",
		};
	if (task.compiled_key)
		return {
			state: "not-compiled",
			label: "Not compiled",
			title:
				last?.reason ||
				"The prompt is not deterministic enough to compile, so runs use the model.",
			tone: "gray",
		};
	return {
		state: "compiling",
		label: "Compiling…",
		title:
			"Its script is compiled in the background; runs until then compile first.",
		tone: "gray",
	};
}

/** The line a Lua error such as `<string>:52: …` points at, or 0. */
export function errorLine(text = ""): number {
	const m = /<string>:(\d+)/.exec(text);
	return m ? Number(m[1]) : 0;
}

export type LuaTokenKind =
	| "keyword"
	| "string"
	| "comment"
	| "number"
	| "builtin"
	| "text";

export interface LuaToken {
	kind: LuaTokenKind;
	text: string;
}

const LUA_KEYWORDS = new Set(
	"and break do else elseif end false for function goto if in local nil not or repeat return then true until while".split(
		" ",
	),
);
const LUA_BUILTINS = new Set(
	"print tool trigger json string table math os pairs ipairs tostring tonumber type error pcall assert select next".split(
		" ",
	),
);
const LUA_TOKEN =
	/(--\[(=*)\[[\s\S]*?\]\2\]|--[^\n]*)|("(?:[^"\\\n]|\\.)*"?|'(?:[^'\\\n]|\\.)*'?|\[(=*)\[[\s\S]*?\]\4\])|(\b0x[\da-fA-F]+\b|\b\d+(?:\.\d+)?(?:[eE][+-]?\d+)?\b)|([A-Za-z_]\w*)/g;

/** A Lua script's lines as highlighted tokens, for the compiled script panel. */
export function highlightLua(script: string): LuaToken[][] {
	const tokens: LuaToken[] = [];
	let at = 0;
	for (const m of script.matchAll(LUA_TOKEN)) {
		const index = m.index ?? 0;
		if (index > at) tokens.push({ kind: "text", text: script.slice(at, index) });
		const [text] = m;
		let kind: LuaTokenKind = "text";
		if (m[1]) kind = "comment";
		else if (m[3]) kind = "string";
		else if (m[5]) kind = "number";
		else if (m[6])
			kind = LUA_KEYWORDS.has(text)
				? "keyword"
				: LUA_BUILTINS.has(text)
					? "builtin"
					: "text";
		tokens.push({ kind, text });
		at = index + text.length;
	}
	if (at < script.length) tokens.push({ kind: "text", text: script.slice(at) });
	// Split into lines, breaking tokens (block comments, long strings) that span them.
	const lines: LuaToken[][] = [[]];
	for (const token of tokens) {
		const parts = token.text.split("\n");
		parts.forEach((part, i) => {
			if (i > 0) lines.push([]);
			if (part) lines[lines.length - 1].push({ kind: token.kind, text: part });
		});
	}
	return lines;
}
