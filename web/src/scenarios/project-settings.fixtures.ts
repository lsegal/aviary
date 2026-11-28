// Shared fixtures for the project-settings-*.component.test.ts files,
// ported from web/e2e/project-settings.spec.ts (#1636).
import { expect } from "vitest";
import { type Locator, page } from "vitest/browser";
import { mockMCP } from "../component-test/server";

export const STATUS = {
	identity: "7F3CA91B",
	issue_filter: "assignee:@me",
	projects: [{ name: "lsegal/aviary", running: 0, waiting: 0, jobs: 0 }],
};

// Shapes follow internal/config/projects.go and internal/projects/api.go.
// A project lives in the projects: list of the agent that owns it; the
// settings every project runs under are the one top-level projects: section.
export const CODER_PROJECTS = [{ target: "lsegal/aviary" }];
export const PROJECTS = {
	allowed_commenters: ["lsegal", "teammate"],
	webhook: { path: "/hooks/gh" },
};
export const CONFIG = {
	server: { port: 16677 },
	agents: [
		{ name: "atlas", model: "anthropic/claude-sonnet-4-5" },
		{
			name: "coder",
			model: "cli/claude/opus",
			fallbacks: ["cli/codex/gpt-5.5"],
			projects: CODER_PROJECTS,
		},
	],
	projects: PROJECTS,
};

export const CLIS = {
	clis: [
		{
			name: "claude",
			installed: true,
			auth: "signed in",
			quota: "38% of 5h quota left",
			models: ["claude/opus", "claude/sonnet"],
			status: "ok",
		},
		{
			name: "codex",
			installed: true,
			auth: "signed in",
			quota: "quota ok",
			models: ["codex/gpt-5.5", "codex/gpt-5.5-mini"],
			status: "ok",
		},
		{ name: "gemini", installed: true, auth: "signed out", status: "warn" },
	],
};

export const READY_CHECK = {
	target: "lsegal/zvidlib",
	kind: "repo",
	fork: true,
	ok: true,
	checks: [
		{
			name: "github",
			status: "ok",
			message: "gh is signed in and can read lsegal/zvidlib",
		},
		{
			name: "fork",
			status: "warn",
			message:
				"You can't push to lsegal/zvidlib, so pull requests will come from your fork",
		},
		{ name: "clis", status: "ok", message: "Found claude and codex" },
		{
			name: "skills",
			status: "ok",
			message: "gh-fix 1.6.0 installed for all 2",
		},
	],
};

export type Args = Record<string, unknown>;
export type Calls = Record<string, Args[]>;

export const CLONES = {
	roots: ["/tmp"],
	clones: [
		{
			path: "/tmp/aviary-209",
			repository: "lsegal/aviary",
			issue: 209,
			inUse: false,
			reason: "not owned by this instance: no job here records it",
			size: 157286400,
		},
		{
			path: "/tmp/aviary-218",
			repository: "lsegal/aviary",
			issue: 218,
			pullRequest: 233,
			inUse: true,
			reason: "job is paused",
		},
		{
			path: "/elsewhere/aviary-230",
			repository: "lsegal/aviary",
			issue: 230,
			inUse: false,
			kept: true,
			reason: "outside the configured temp directories; remove it by hand",
			size: 0,
		},
	],
	totalSize: 157286400,
	unmeasured: 1,
};

export function setup(
	opts: {
		check?: unknown;
		config?: unknown;
		configDelay?: number;
		jobs?: { number: number; status: string }[];
		clonesError?: string;
		clones?: typeof CLONES;
		removeError?: string;
		sessions?: object[];
	} = {},
) {
	const calls: Calls = {};
	const record = (tool: string, args?: Args) => {
		calls[tool] ??= [];
		calls[tool].push(args ?? {});
	};
	mockMCP({
		agent_list: [],
		auth_list: [],
		session_list: opts.sessions ?? [],
		tool_list: [],
		agent_file_list: [],
		project_status: STATUS,
		project_job_list: (args?: Args) => {
			record("project_job_list", args);
			return { jobs: opts.jobs ?? [], total: opts.jobs?.length ?? 0 };
		},
		delete_preview: (args?: Args) => {
			record("delete_preview", args);
			return { sessions: 0, messages: 0, runs: 12, files: 5 };
		},
		project_remove: (args?: Args) => {
			record("project_remove", args);
			if (opts.removeError) throw new Error(opts.removeError);
			return {
				target: args?.target,
				removed: true,
				stopped_jobs: (opts.jobs ?? []).map((j) => j.number),
			};
		},
		config_get: async () => {
			if (opts.configDelay)
				await new Promise((r) => setTimeout(r, opts.configDelay));
			return opts.config ?? CONFIG;
		},
		config_save: (args?: Args) => {
			record("config_save", args);
			return "saved";
		},
		project_check: (args?: Args) => {
			record("project_check", args);
			if (typeof opts.check === "function") return opts.check(args);
			if (opts.check) return opts.check;
			// The checks normalize the target, as project_check does.
			const target = String(args?.target ?? "").replace(
				"https://github.com/",
				"",
			);
			return { ...READY_CHECK, target };
		},
		project_add: (args?: Args) => {
			record("project_add", args);
			return { ok: true };
		},
		provider_cli_list: async () => CLIS,
		project_clone_list: () => {
			if (opts.clonesError) throw new Error(opts.clonesError);
			return opts.clones ?? CLONES;
		},
		project_clone_clear: (args?: Args) => {
			record("project_clone_clear", args);
			return {
				removed: [{ path: "/tmp/aviary-209", size: 157286400 }],
				kept: 2,
				reclaimed: 157286400,
			};
		},
	});
	return calls;
}

/** The saved config: what the last config_save wrote. */
export function savedConfig(calls: Calls) {
	return JSON.parse(String(calls.config_save?.at(-1)?.config));
}

/** The projects: list the last config_save wrote under agent. */
export function savedProjects(calls: Calls, agent: string) {
	return savedConfig(calls).agents.find(
		(a: { name: string }) => a.name === agent,
	)?.projects;
}

export const saveChanges = () =>
	page.getByRole("button", { name: "Save Changes" }).click();

// project_check as it answers when gemini lacks gh-fix: a failure when the
// models it is given run on gemini, a neutral note otherwise.
export function geminiSkillCheck(args?: Args) {
	const usesGemini = (args?.models as string[] | undefined)?.some((m) =>
		m.includes("gemini"),
	);
	return {
		target: "lsegal/zvidlib",
		kind: "repo",
		ok: !usesGemini,
		checks: [
			{ name: "clis", status: "ok", message: "Found claude and gemini" },
			usesGemini
				? {
						name: "skills",
						status: "fail",
						message: "gh-fix is not installed for gemini",
					}
				: {
						name: "skills",
						status: "ok",
						message: "gh-fix 1.6.0 installed for claude",
					},
			...(usesGemini
				? []
				: [
						{
							name: "skills_unused",
							status: "info",
							message: "gh-fix also missing for gemini (not used)",
						},
					]),
		],
	};
}

export const REPORTS_CONFIG = {
	...CONFIG,
	agents: [
		{
			name: "atlas",
			model: "anthropic/claude-sonnet-4-5",
			channels: [
				{ type: "slack", id: "main", name: "Team Slack", token: "xoxb-test" },
			],
		},
		{ name: "scout", model: "anthropic/claude-sonnet-4-5" },
		{
			name: "coder",
			model: "cli/claude/opus",
			projects: [{ target: "lsegal/aviary", other: "kept" }],
		},
	],
};

export function count(loc: Locator): number {
	return loc.elements().length;
}

/** Playwright's toBeHidden: absent, or present but not visible. */
export async function expectHidden(loc: Locator) {
	await expect
		.poll(() => loc.elements().filter((el) => el.checkVisibility()).length)
		.toBe(0);
}

/** Playwright's check()/uncheck(): clicks a checkbox or radio into state. */
export async function setChecked(loc: Locator, checked = true) {
	await expect.element(loc).toBeInTheDocument();
	if ((loc.element() as HTMLInputElement).checked !== checked)
		await loc.click();
	if (checked) await expect.element(loc).toBeChecked();
	else await expect.element(loc).not.toBeChecked();
}
