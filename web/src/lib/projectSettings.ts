// Project settings and the add-project flow. A project belongs to exactly one
// Aviary agent: it is listed in that agent's `projects:` section of
// aviary.yaml, whose entries carry each project's optional job reports. The
// settings every project runs under are one shared set, the top-level
// `projects:` section, which holds only values that differ from the defaults
// below, mirroring internal/config/projects.go. Both are read and written
// with config_get / config_save.
// The add-project checks come from project_check and clones from
// project_clone_list; their responses are normalized here so the views never
// depend on the raw payload shape.

import type { ApprovalMode } from "../stores/settings";
import type { ProjectMode, ProjectModeChoice } from "./projects";

export type Delivery = "browser" | "poll" | "webhook";

/** A project's opt-in to job reports through an Aviary agent's channels. */
export interface ProjectReportsEntry {
	agent: string;
	targets: string[];
	/** Reported events; absent reports every one of REPORT_EVENTS. */
	events?: string[];
}

/** One watched project in an agent's `projects:` list. */
export interface ProjectEntry {
	target: string;
	reports?: ProjectReportsEntry;
	// Other per-project settings pass through untouched.
	[key: string]: unknown;
	/** The project's own mode, overriding the shared one (#1371); unset
	 * follows the shared mode. "manual" never searches for issues (#1307). */
	mode?: ProjectMode;
}

/** The reports form. With enabled false nothing is saved. */
export interface ProjectReports {
	enabled: boolean;
	agent: string;
	targets: string[];
	events: string[];
}

/** An agent's `projects:` as stored in aviary.yaml: the projects it owns. */
export type AgentProjectsSection = ProjectEntry[];

/** The shared top-level `projects:` settings as stored in aviary.yaml (snake_case, sparse). */
export interface ProjectsSection {
	concurrency?: number;
	interval?: string;
	delivery?: string;
	browser_vision?: boolean;
	browser_visible?: boolean;
	ready_state?: string;
	filter?: string[];
	all_issues?: boolean;
	allowed_commenters?: string[];
	/** The mode of every project without its own; unset means watch. */
	mode?: ProjectMode;
	no_merge?: boolean;
	no_changelog?: boolean;
	approval_mode?: ApprovalMode;
	/** The full approval mode, read when approval_mode is unset; saving folds it into approval_mode. */
	yolo?: boolean;
	/** CLI provider model every project's jobs run on in place of the owning agent's. */
	model?: string;
	/** Fallbacks of model, in order; ignored without model. */
	fallbacks?: string[];
	unblock_interval?: string;
	temp_dirs?: string[];
	// Fields this page doesn't edit (webhook, unblock_agent, ...) pass through untouched.
	[key: string]: unknown;
}

/** The shared projects settings, with defaults filled in. */
export interface ProjectSettings {
	concurrency: number;
	readyState: string;
	delivery: Delivery;
	/** Browser delivery's capped screenshot fallback. */
	browserVision: boolean;
	/** Shows the browser browser delivery reads with, for debugging. */
	browserVisible: boolean;
	interval: string;
	filter: string[];
	allIssues: boolean;
	commenters: string[];
	/** The mode of every project without its own. */
	mode: ProjectMode;
	merge: boolean;
	changelog: boolean;
	/** Projects' approval-mode override; empty follows the general approval mode. */
	approvalMode: ApprovalMode | "";
	/** Jobs' model override; empty keeps each owning agent's model and fallbacks. */
	model: string;
	fallbacks: string[];
	unblockInterval: string;
	tempDirs: string[];
}

export const DEFAULT_CONCURRENCY = 3;
export const DEFAULT_DELIVERY: Delivery = "browser";
export const DEFAULT_INTERVAL = "30s";
export const DEFAULT_BROWSER_INTERVAL = "20s";

/** The poll interval a delivery mode uses when none is set. */
export function defaultInterval(delivery: Delivery): string {
	return delivery === "browser" ? DEFAULT_BROWSER_INTERVAL : DEFAULT_INTERVAL;
}
export const DEFAULT_FILTER = ["assignee:@me"];

// The job events a project can report, from config.ProjectReportEvents.
export const REPORT_EVENTS: { key: string; label: string }[] = [
	{ key: "merged", label: "Merged" },
	{ key: "failed", label: "Failed" },
	{ key: "waiting", label: "Waiting on an answer" },
];

export const DELIVERY_OPTIONS: { key: Delivery; label: string }[] = [
	{ key: "browser", label: "Aviary's browser, signed in as you" },
	{ key: "poll", label: "Poll with gh" },
	{ key: "webhook", label: "Webhook (ngrok tunnel)" },
];

function strings(v: unknown): string[] {
	return Array.isArray(v)
		? v
				.filter((s): s is string => typeof s === "string")
				.map((s) => s.trim())
				.filter(Boolean)
		: [];
}

function sameTarget(a: string | undefined, b: string): boolean {
	return (a ?? "").trim().toLowerCase() === b.trim().toLowerCase();
}

/** Whether target is among the watched projects. */
export function isListed(
	section: AgentProjectsSection | undefined,
	target: string,
): boolean {
	return (section ?? []).some((p) => sameTarget(p.target, target));
}

/** The watched targets, in order. */
export function projectTargets(
	section: AgentProjectsSection | undefined,
): string[] {
	return (section ?? []).map((p) => (p.target ?? "").trim()).filter(Boolean);
}

/** The parts of the Aviary config a project's job models come from. */
export interface JobModelsConfig {
	agents?: {
		name: string;
		model?: string;
		fallbacks?: string[];
		projects?: AgentProjectsSection;
	}[];
	models?: { defaults?: { model?: string; fallbacks?: string[] } };
	projects?: { model?: string; fallbacks?: string[]; mode?: string };
}

/**
 * The job model override, projects.model then projects.fallbacks, with
 * provider:model shorthands such as claude:opus written as claude/opus, as
 * config.NormalizeProjectModel does. Empty when projects.model is unset.
 */
export function jobModelOverride(
	cfg: JobModelsConfig | null | undefined,
	cliProviders: readonly string[] = [],
): string[] {
	const normalize = (m: string) => {
		const v = m.trim();
		const [provider, ...rest] = v.split(":");
		return !v.includes("/") && rest.length && cliProviders.includes(provider)
			? `${provider}/${rest.join(":")}`
			: v;
	};
	const model = normalize(cfg?.projects?.model ?? "");
	if (!model) return [];
	return [model, ...strings(cfg?.projects?.fallbacks).map(normalize)];
}

/**
 * The name of the agent that owns target, mirroring config.ProjectOwner: the
 * first agent whose projects list it. "" when no agent does, as for a runtime
 * watch.
 */
export function projectOwner(
	cfg: JobModelsConfig | null | undefined,
	target: string,
): string {
	const owner = (cfg?.agents ?? []).find((a) => isListed(a.projects, target));
	return owner?.name ?? "";
}

/** Every agent's projects and the settings they share. */
export const PROJECTS_SETTINGS_PATH = "/projects/settings";

/** The settings path where target's owning agent manages it. */
export function projectSettingsPath(
	cfg: JobModelsConfig | null | undefined,
	target: string,
): string {
	const owner = projectOwner(cfg, target);
	if (!owner) return PROJECTS_SETTINGS_PATH;
	return agentProjectsSettingsPath(owner, target);
}

/** The projects settings opened on an agent's projects, one of them selected. */
export function agentProjectsSettingsPath(agent: string, target = ""): string {
	const query = new URLSearchParams({ agent });
	if (target) query.set("project", target);
	return `${PROJECTS_SETTINGS_PATH}?${query}`;
}

/** The projects with target added, unless already listed. */
export function withProject(
	section: AgentProjectsSection | undefined,
	target: string,
): AgentProjectsSection {
	const t = target.trim();
	const out = section ?? [];
	if (!t || isListed(out, t)) return out;
	return [...out, { target: t }];
}

/** The projects with target removed. */
export function withoutProject(
	section: AgentProjectsSection | undefined,
	target: string,
): AgentProjectsSection {
	return (section ?? []).filter((p) => !sameTarget(p.target, target));
}

/** One model a project's jobs run on, in fallback order. */
export interface JobModel {
	/** The model spec (AGENT/MODEL:LEVEL), or aviary/<agent>. */
	spec: string;
	/** The agent model it comes from, such as claude/opus or cli/claude/opus. */
	model: string;
}

/**
 * The models a project owned by agentName runs its jobs on, mirroring
 * config.ProjectJobSpecs: the job model override when projects.model is set,
 * of CLI provider models only, else the agent's model and fallbacks, after
 * models.defaults, each CLI provider model (claude/opus or cli/claude/opus)
 * as its spec. cliProviders names the CLI providers. A model that is not a CLI
 * runs the job on the agent itself. An empty name takes the CLI models of
 * models.defaults; an unknown agent has none.
 */
export function jobModels(
	agentName: string,
	cfg: JobModelsConfig | null | undefined,
	cliProviders: readonly string[] = [],
): JobModel[] {
	const name = agentName.trim();
	const agent = name
		? (cfg?.agents ?? []).find((a) => a.name === name)
		: { name: "" };
	if (!agent) return [];
	const override = jobModelOverride(cfg, cliProviders);
	if (override.length) {
		const out: JobModel[] = [];
		for (const m of override) {
			const spec = m.startsWith("cli/") ? m.slice("cli/".length) : m;
			if (
				cliProviders.includes(spec.split("/")[0]) &&
				!out.some((j) => j.spec === spec)
			)
				out.push({ spec, model: m });
		}
		return out;
	}
	const defaults = cfg?.models?.defaults;
	const model = (agent.model ?? "").trim() || (defaults?.model ?? "").trim();
	const fallbacks = agent.fallbacks?.length
		? agent.fallbacks
		: (defaults?.fallbacks ?? []);
	const out: JobModel[] = [];
	for (const m of [model, ...fallbacks].map((v) => v.trim()).filter(Boolean)) {
		const spec = m.startsWith("cli/")
			? m.slice("cli/".length)
			: cliProviders.includes(m.split("/")[0])
				? m
				: agent.name
					? `aviary/${agent.name}`
					: "";
		if (spec && !out.some((j) => j.spec === spec)) out.push({ spec, model: m });
	}
	return out;
}

function parseDelivery(v: string | undefined): Delivery {
	const mode = (v ?? "").trim().toLowerCase();
	return mode === "poll" || mode === "webhook" ? mode : DEFAULT_DELIVERY;
}

function parseMode(v: unknown): ProjectMode | undefined {
	const mode = typeof v === "string" ? v.trim().toLowerCase() : "";
	return mode === "watch" || mode === "manual" ? mode : undefined;
}

/** The shared mode of a config, which projects without their own follow. */
export function globalProjectMode(
	cfg: JobModelsConfig | null | undefined,
): ProjectMode {
	return parseMode(cfg?.projects?.mode) ?? "watch";
}

/** Reads the shared projects settings from their section, filling in defaults. */
export function settingsFromSection(
	section: ProjectsSection | undefined,
): ProjectSettings {
	const s = section ?? {};
	const filter = strings(s.filter);
	return {
		concurrency:
			typeof s.concurrency === "number" && s.concurrency > 0
				? s.concurrency
				: DEFAULT_CONCURRENCY,
		readyState: (s.ready_state ?? "").trim(),
		delivery: parseDelivery(s.delivery),
		browserVision: s.browser_vision === true,
		browserVisible: s.browser_visible === true,
		interval: (s.interval ?? "").trim(),
		filter: filter.length ? filter : [...DEFAULT_FILTER],
		allIssues: s.all_issues === true,
		commenters: strings(s.allowed_commenters),
		mode: parseMode(s.mode) ?? "watch",
		merge: s.no_merge !== true,
		changelog: s.no_changelog !== true,
		approvalMode: s.approval_mode ?? (s.yolo === true ? "full" : ""),
		model: (s.model ?? "").trim(),
		fallbacks: strings(s.fallbacks),
		unblockInterval: (s.unblock_interval ?? "").trim(),
		tempDirs: strings(s.temp_dirs),
	};
}

/** Reads target's report settings from the agent's projects. */
export function reportsFromSection(
	section: AgentProjectsSection | undefined,
	target: string,
): ProjectReports {
	const entry = (section ?? []).find((p) => sameTarget(p.target, target));
	return reportsFromEntry(entry?.reports);
}

function reportsFromEntry(r: ProjectReportsEntry | undefined): ProjectReports {
	const agent = (r?.agent ?? "").trim();
	const targets = strings(r?.targets);
	const events = strings(r?.events).map((e) => e.toLowerCase());
	return {
		// Matches ProjectReportsConfig.Enabled: an agent and a target.
		enabled: !!agent && targets.length > 0,
		agent,
		targets,
		events: events.length ? events : REPORT_EVENTS.map((e) => e.key),
	};
}

// Target rows not filled in yet (a session row with no session) are left out
// of what's saved.
function filledTargets(targets: string[]): string[] {
	return clean(targets).filter((t) => {
		const { channel, id } = splitReportTarget(t);
		return channel.startsWith("channel:") || !!id;
	});
}

/** The `reports` entry to save, or undefined while reports are off. */
function reportsEntry(r: ProjectReports): ProjectReportsEntry | undefined {
	if (!r.enabled) return undefined;
	const events = clean(r.events);
	const all = REPORT_EVENTS.every((e) => events.includes(e.key));
	return {
		agent: r.agent.trim(),
		targets: filledTargets(r.targets),
		...(all ? {} : { events }),
	};
}

/**
 * Why the reports can't be saved, or "" when they can. Mirrors
 * config.ValidateProjectReports: the agent must be configured and at least one
 * target is needed. Events come from fixed checkboxes, so at least one must be
 * picked; none would be saved as no filter, which reports every event.
 */
export function reportsError(r: ProjectReports, agents: string[]): string {
	if (!r.enabled) return "";
	const agent = r.agent.trim();
	if (!agent) return "Choose the Aviary agent that sends reports.";
	if (!agents.includes(agent))
		return `Reports agent "${agent}" is not configured.`;
	const targets = filledTargets(r.targets);
	if (!targets.length) return "Reports need at least one target.";
	if (targets.some((t) => !splitReportTarget(t).channel))
		return "Choose a channel for each target.";
	const events = clean(r.events);
	const unknown = events.find((e) => !REPORT_EVENTS.some((k) => k.key === e));
	if (unknown) return `Unknown report event "${unknown}".`;
	if (!events.length) return "Choose at least one event to report.";
	return "";
}

/**
 * A report target split into its choice and the session it names: a channel
 * by name ("channel:<name>", complete as is) or `session` with a session.
 * Anything else is unrecognized and kept as id with no channel.
 */
export function splitReportTarget(target: string): {
	channel: string;
	id: string;
} {
	const raw = target.trim();
	if (raw.startsWith("session:"))
		return { channel: "session", id: raw.slice("session:".length) };
	if (raw.startsWith("channel:")) return { channel: raw, id: "" };
	return { channel: "", id: raw };
}

/** Joins a target choice and session back into a report target. */
export function joinReportTarget(channel: string, id: string): string {
	const c = channel.trim();
	const v = id.trim();
	if (c.startsWith("channel:")) return c;
	if (c === "session") return v ? `session:${v}` : "";
	return v;
}

function sameList(a: string[], b: string[]): boolean {
	return a.length === b.length && a.every((v, i) => v === b[i]);
}

function clean(list: string[]): string[] {
	return list.map((v) => v.trim()).filter(Boolean);
}

/**
 * Writes the settings back into the section, keeping only values that differ
 * from their defaults.
 */
export function withSettings(
	section: ProjectsSection | undefined,
	s: ProjectSettings,
): ProjectsSection {
	const out: ProjectsSection = { ...(section ?? {}) };
	const set = (key: string, value: unknown, isDefault: boolean) => {
		if (isDefault) delete out[key];
		else out[key] = value;
	};
	set(
		"concurrency",
		Math.floor(s.concurrency),
		!(s.concurrency > 0) || s.concurrency === DEFAULT_CONCURRENCY,
	);
	set("ready_state", s.readyState.trim(), !s.readyState.trim());
	set("delivery", s.delivery, s.delivery === DEFAULT_DELIVERY);
	// The browser-only switches mean nothing for the API-backed modes.
	const browser = s.delivery === "browser";
	set("browser_vision", true, !browser || !s.browserVision);
	set("browser_visible", true, !browser || !s.browserVisible);
	const interval = s.interval.trim();
	set(
		"interval",
		interval,
		!interval || interval === defaultInterval(s.delivery),
	);
	const filter = clean(s.filter);
	set("filter", filter, !filter.length || sameList(filter, DEFAULT_FILTER));
	set("all_issues", true, !s.allIssues);
	const commenters = clean(s.commenters);
	set("allowed_commenters", commenters, !commenters.length);
	set("mode", s.mode, s.mode !== "manual");
	set("no_merge", true, s.merge);
	set("no_changelog", true, s.changelog);
	set("approval_mode", s.approvalMode, !s.approvalMode);
	set("yolo", true, true);
	// Fallbacks apply only with a model, as projects.fallbacks does.
	const model = s.model.trim();
	const fallbacks = clean(s.fallbacks);
	set("model", model, !model);
	set("fallbacks", fallbacks, !model || !fallbacks.length);
	set("unblock_interval", s.unblockInterval.trim(), !s.unblockInterval.trim());
	const dirs = clean(s.tempDirs);
	set("temp_dirs", dirs, !dirs.length);
	return out;
}

/** Writes target's report settings into its entry in the agent's projects. */
export function withReports(
	section: AgentProjectsSection | undefined,
	target: string,
	r: ProjectReports,
): AgentProjectsSection {
	const reports = reportsEntry(r);
	return (section ?? []).map((p) => {
		if (!sameTarget(p.target, target)) return p;
		// Keep the entry's other fields; only reports change here.
		const { reports: _, ...rest } = p;
		return reports ? { ...rest, reports } : rest;
	});
}

/** Target's effective mode: its own override, else the shared defaultMode. */
export function projectMode(
	section: AgentProjectsSection | undefined,
	target: string,
	defaultMode: ProjectMode,
): ProjectMode {
	const entry = (section ?? []).find((p) => sameTarget(p.target, target));
	return parseMode(entry?.mode) ?? defaultMode;
}

/** The projects with target's mode override set, or removed for "inherit". */
export function withMode(
	section: AgentProjectsSection | undefined,
	target: string,
	mode: ProjectModeChoice,
): AgentProjectsSection {
	return (section ?? []).map((p) => {
		if (!sameTarget(p.target, target)) return p;
		const { mode: _, ...rest } = p;
		return mode === "inherit" ? rest : { ...rest, mode };
	});
}

/** Which settings still hold their defaults, for the grey "default" tags. */
export function defaultFields(s: ProjectSettings): Set<keyof ProjectSettings> {
	const d = settingsFromSection({});
	const out = new Set<keyof ProjectSettings>();
	for (const key of Object.keys(d) as (keyof ProjectSettings)[]) {
		if (JSON.stringify(s[key]) === JSON.stringify(d[key])) out.add(key);
	}
	return out;
}

/** Splits a comma- or whitespace-separated list typed into one field. */
export function parseList(text: string): string[] {
	return text
		.split(/[\s,]+/)
		.map((v) => v.trim().replace(/^@/, ""))
		.filter(Boolean);
}

function yamlScalar(v: unknown): string {
	if (typeof v !== "string") return String(v);
	if (
		v === "" ||
		/^\s|\s$|[:#{}[\],&*!|>'"%@`]|^(true|false|null|yes|no|~|[-+]?\d)/i.test(v)
	)
		return JSON.stringify(v);
	return v;
}

function yamlValue(indent: string, key: string, value: unknown): string[] {
	if (Array.isArray(value)) {
		if (value.every((v) => typeof v !== "object" || v === null))
			return [`${indent}${key}: [${value.map(yamlScalar).join(", ")}]`];
		const lines = [`${indent}${key}:`];
		for (const item of value) {
			const entries = Object.entries(item as Record<string, unknown>);
			entries.forEach(([k, v], i) => {
				const lead = i === 0 ? `${indent}  - ` : `${indent}    `;
				lines.push(...yamlValue(lead, k, v));
			});
		}
		return lines;
	}
	if (value && typeof value === "object") {
		const lines = [`${indent}${key}:`];
		const child = `${" ".repeat(indent.length)}  `;
		for (const [k, v] of Object.entries(value))
			lines.push(...yamlValue(child, k, v));
		return lines;
	}
	return [`${indent}${key}: ${yamlScalar(value)}`];
}

/**
 * Renders an agent's `projects:` list as it will be written under the
 * agent, or "" when empty.
 */
export function agentProjectsYAML(
	agent: string,
	section: AgentProjectsSection,
): string {
	if (!section.length) return "";
	return [
		"agents:",
		`  - name: ${yamlScalar(agent)}`,
		...yamlValue("    ", "projects", section),
	].join("\n");
}

/**
 * Renders the shared `projects:` settings as they will be written at the top
 * level, or "" when every setting is a default.
 */
export function projectsYAML(section: ProjectsSection): string {
	const entries = Object.entries(section).filter(([, v]) => v !== undefined);
	if (!entries.length) return "";
	return [
		"projects:",
		...entries.flatMap(([k, v]) => yamlValue("  ", k, v)),
	].join("\n");
}

// ── Add-project checks (project_check) ────────────────────────────────────

/** info is a neutral note, such as skills missing on CLIs the project never uses. */
export type CheckStatus = "ok" | "warn" | "fail" | "info";

export interface ProjectCheckItem {
	name: string;
	status: CheckStatus;
	message: string;
}

export interface ProjectCheckResult {
	target: string;
	kind: string;
	fork: boolean;
	ok: boolean;
	checks: ProjectCheckItem[];
}

const CHECK_LABELS: Record<string, string> = {
	github: "GitHub",
	clis: "CLI providers",
	skills: "Skills",
	skills_unused: "Skills",
	fork: "Fork",
};

export function checkLabel(name: string): string {
	return CHECK_LABELS[name] ?? name;
}

type Raw = Record<string, unknown>;

function obj(v: unknown): Raw {
	return v && typeof v === "object" && !Array.isArray(v) ? (v as Raw) : {};
}

function str(v: unknown): string {
	return typeof v === "string" ? v : typeof v === "number" ? String(v) : "";
}

export function normalizeCheck(raw: unknown): ProjectCheckResult {
	const r = obj(raw);
	const checks = (Array.isArray(r.checks) ? r.checks : []).map((c) => {
		const o = obj(c);
		const status = str(o.status);
		return {
			name: str(o.name),
			status: (status === "ok" || status === "warn" || status === "info"
				? status
				: "fail") as CheckStatus,
			message: str(o.message),
		};
	});
	return {
		target: str(r.target),
		kind: str(r.kind),
		fork: r.fork === true,
		ok: r.ok === true && !checks.some((c) => c.status === "fail"),
		checks,
	};
}

// ── Clones (project_clone_list / project_clone_clear) ─────────────────────

export interface ProjectClone {
	path: string;
	repo: string;
	issue: number;
	pr: number;
	/** A live job of this instance holds it, so clearing keeps it. */
	inUse: boolean;
	/** Nothing holds it, but clearing leaves it alone (e.g. outside the temp directories). */
	kept: boolean;
	/** Why it is in use, kept, or unused. */
	reason: string;
	/** Bytes on disk, or null when it could not be measured in time. */
	size: number | null;
}

function normalizeClone(c: unknown): ProjectClone {
	const o = obj(c);
	return {
		path: str(o.path),
		repo: str(o.repository ?? o.repo),
		issue: Number(o.issue) || 0,
		pr: Number(o.pullRequest ?? o.pr) || 0,
		inUse: o.inUse === true || o.in_use === true,
		kept: o.kept === true,
		reason: str(o.reason),
		size: typeof o.size === "number" && o.size >= 0 ? o.size : null,
	};
}

/** The state a clone row shows beside its reason. */
export function cloneState(c: ProjectClone): "in use" | "kept" | "unused" {
	if (c.inUse) return "in use";
	return c.kept ? "kept" : "unused";
}

export function normalizeClones(raw: unknown): {
	roots: string[];
	clones: ProjectClone[];
	totalSize: number;
	unmeasured: number;
} {
	const r = obj(raw);
	return {
		roots: strings(r.roots),
		clones: (Array.isArray(r.clones) ? r.clones : [])
			.map(normalizeClone)
			.filter((c) => c.path),
		totalSize: Number(r.totalSize) || 0,
		unmeasured: Number(r.unmeasured) || 0,
	};
}

export interface ClearResult {
	removed: number;
	kept: number;
	reclaimed: number;
	errors: string[];
}

export function normalizeClear(raw: unknown): ClearResult {
	const r = obj(raw);
	return {
		removed: Array.isArray(r.removed) ? r.removed.length : 0,
		kept: Number(r.kept) || 0,
		reclaimed: Number(r.reclaimed) || 0,
		errors: strings(r.errors),
	};
}

export function formatBytes(n: number): string {
	if (n < 1024) return `${n} B`;
	const units = ["KB", "MB", "GB", "TB"];
	let v = n / 1024;
	let i = 0;
	while (v >= 1024 && i < units.length - 1) {
		v /= 1024;
		i++;
	}
	return `${v >= 10 ? Math.round(v) : v.toFixed(1)} ${units[i]}`;
}
