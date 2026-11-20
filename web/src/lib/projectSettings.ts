// Project settings and the add-project flow. Settings live in the `projects:`
// section of aviary.yaml (read and written with config_get / config_save) and
// hold only values that differ from the defaults below, which mirror
// internal/config/projects.go. Most settings are shared by every watched
// project; each project's own entry in `list` carries the Aviary agent it is
// attached to and its optional job reports.
// The add-project checks come from project_check and clones from
// project_clone_list; their responses are normalized here so the views never
// depend on the raw payload shape.

export type Delivery = "browser" | "poll" | "webhook";

/** A project's opt-in to job reports through an Aviary agent's channels. */
export interface ProjectReportsEntry {
	agent: string;
	targets: string[];
	/** Reported events; absent reports every one of REPORT_EVENTS. */
	events?: string[];
}

/** One watched project in `projects.list`. */
export interface ProjectEntry {
	target: string;
	/** The Aviary agent this project is attached to; absent uses the section's. */
	agent?: string;
	reports?: ProjectReportsEntry;
	// Other per-project settings pass through untouched.
	[key: string]: unknown;
}

/** The reports form. With enabled false nothing is saved. */
export interface ProjectReports {
	enabled: boolean;
	agent: string;
	targets: string[];
	events: string[];
}

/** The `projects:` section as stored in aviary.yaml (snake_case, sparse). */
export interface ProjectsSection {
	list?: ProjectEntry[];
	concurrency?: number;
	interval?: string;
	delivery?: string;
	browser_vision?: boolean;
	browser_visible?: boolean;
	ready_label?: string;
	ready_state?: string;
	filter?: string[];
	all_issues?: boolean;
	allowed_commenters?: string[];
	/** The Aviary agent projects without their own are attached to. */
	agent?: string;
	no_merge?: boolean;
	no_changelog?: boolean;
	yolo?: boolean;
	unblock_interval?: string;
	temp_dirs?: string[];
	// Fields this page doesn't edit (webhook, unblock_agent, ...) pass through untouched.
	[key: string]: unknown;
}

/** The settings edited on one project's page, with defaults filled in. */
export interface ProjectSettings {
	target: string;
	/** The Aviary agent this project is attached to; empty uses the shared `agent`. */
	agent: string;
	concurrency: number;
	readyLabel: string;
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
	merge: boolean;
	changelog: boolean;
	yolo: boolean;
	unblockInterval: string;
	tempDirs: string[];
	reports: ProjectReports;
}

export const DEFAULT_CONCURRENCY = 3;
export const DEFAULT_READY_LABEL = "ready";
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

/** Whether target is in the section's list of watched projects. */
export function isListed(
	section: ProjectsSection | undefined,
	target: string,
): boolean {
	return (section?.list ?? []).some((p) => sameTarget(p.target, target));
}

/** The Aviary agent a project is attached to when it names none of its own. */
export function sharedAgent(section: ProjectsSection | undefined): string {
	return typeof section?.agent === "string" ? section.agent.trim() : "";
}

/** The parts of the Aviary config a project's job models come from. */
export interface JobModelsConfig {
	agents?: { name: string; model?: string; fallbacks?: string[] }[];
	models?: { defaults?: { model?: string; fallbacks?: string[] } };
}

/** One model a project's jobs run on, in fallback order. */
export interface JobModel {
	/** The model spec (AGENT/MODEL:LEVEL), or aviary/<agent>. */
	spec: string;
	/** The agent model it comes from, such as claude/opus or cli/claude/opus. */
	model: string;
}

/**
 * The models a project attached to agentName runs its jobs on, mirroring
 * config.ProjectJobSpecs: the agent's model and fallbacks, after
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

/** Reads one project's settings from the section, filling in defaults. */
export function settingsFromSection(
	target: string,
	section: ProjectsSection | undefined,
): ProjectSettings {
	const s = section ?? {};
	const entry = (s.list ?? []).find((p) => sameTarget(p.target, target));
	const filter = strings(s.filter);
	return {
		target: entry?.target ?? target,
		agent: typeof entry?.agent === "string" ? entry.agent.trim() : "",
		concurrency:
			typeof s.concurrency === "number" && s.concurrency > 0
				? s.concurrency
				: DEFAULT_CONCURRENCY,
		// An explicit empty ready label turns label matching off.
		readyLabel:
			typeof s.ready_label === "string"
				? s.ready_label.trim()
				: DEFAULT_READY_LABEL,
		readyState: (s.ready_state ?? "").trim(),
		delivery: parseDelivery(s.delivery),
		browserVision: s.browser_vision === true,
		browserVisible: s.browser_visible === true,
		interval: (s.interval ?? "").trim(),
		filter: filter.length ? filter : [...DEFAULT_FILTER],
		allIssues: s.all_issues === true,
		commenters: strings(s.allowed_commenters),
		merge: s.no_merge !== true,
		changelog: s.no_changelog !== true,
		yolo: s.yolo === true,
		unblockInterval: (s.unblock_interval ?? "").trim(),
		tempDirs: strings(s.temp_dirs),
		reports: reportsFromEntry(entry?.reports),
	};
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

// Target rows still missing their delivery ID are left out of what's saved.
function filledTargets(targets: string[]): string[] {
	return clean(targets).filter((t) => splitReportTarget(t).id);
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

/** A report target split into its channel (`session` or TYPE:ID) and delivery id. */
export function splitReportTarget(target: string): {
	channel: string;
	id: string;
} {
	const raw = target.trim();
	if (raw.startsWith("session:"))
		return { channel: "session", id: raw.slice("session:".length) };
	const parts = raw.split(":");
	if (parts.length >= 3)
		return { channel: `${parts[0]}:${parts[1]}`, id: parts.slice(2).join(":") };
	return { channel: "", id: raw };
}

/** Joins a channel and delivery id back into a report target. */
export function joinReportTarget(channel: string, id: string): string {
	const c = channel.trim();
	const v = id.trim();
	if (!c) return v;
	return v ? `${c}:${v}` : "";
}

function sameList(a: string[], b: string[]): boolean {
	return a.length === b.length && a.every((v, i) => v === b[i]);
}

function clean(list: string[]): string[] {
	return list.map((v) => v.trim()).filter(Boolean);
}

/**
 * Writes one project's settings back into the section, keeping only values
 * that differ from their defaults. With listed false the project is left out
 * of `list` unless it has settings of its own, so a project watched with
 * defaults writes nothing.
 */
export function withSettings(
	section: ProjectsSection | undefined,
	s: ProjectSettings,
	listed = isListed(section, s.target),
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
	const label = s.readyLabel.trim();
	set("ready_label", label, label === DEFAULT_READY_LABEL);
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
	set("no_merge", true, s.merge);
	set("no_changelog", true, s.changelog);
	set("yolo", true, !s.yolo);
	set("unblock_interval", s.unblockInterval.trim(), !s.unblockInterval.trim());
	const dirs = clean(s.tempDirs);
	set("temp_dirs", dirs, !dirs.length);

	const agent = s.agent.trim();
	const reports = reportsEntry(s.reports);
	const current = section?.list ?? [];
	const at = current.findIndex((p) => sameTarget(p.target, s.target));
	const list = current.filter((p) => !sameTarget(p.target, s.target));
	// Keep the entry's other fields; only agent and reports change here.
	const {
		agent: _,
		reports: __,
		...rest
	} = at >= 0 ? current[at] : { target: s.target };
	const extra = Object.keys(rest).some((k) => k !== "target");
	if (listed || agent || reports || extra) {
		const entry: ProjectEntry = {
			...rest,
			target: s.target.trim(),
			...(agent ? { agent } : {}),
			...(reports ? { reports } : {}),
		};
		list.splice(at >= 0 ? at : list.length, 0, entry);
	}
	set("list", list, !list.length);
	return out;
}

/** Which settings still hold their defaults, for the grey "default" tags. */
export function defaultFields(s: ProjectSettings): Set<keyof ProjectSettings> {
	const d = settingsFromSection(s.target, {});
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

/** Renders the `projects:` section as it will be written, or "" when empty. */
export function projectsYAML(section: ProjectsSection): string {
	const entries = Object.entries(section).filter(([, v]) => v !== undefined);
	if (!entries.length) return "";
	return [
		"projects:",
		...entries.flatMap(([k, v]) => yamlValue("  ", k, v)),
	].join("\n");
}

// ── Add-project checks (project_check) ────────────────────────────────────

export type CheckStatus = "ok" | "warn" | "fail";

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
	ready_label: "Ready label",
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
			status: (status === "ok" || status === "warn"
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
	inUse: boolean;
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
		reason: str(o.reason),
		size: typeof o.size === "number" && o.size >= 0 ? o.size : null,
	};
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
