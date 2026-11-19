// Project settings and the add-project flow. Settings live in the `projects:`
// section of aviary.yaml (read and written with config_get / config_save) and
// hold only values that differ from the defaults below, which mirror
// internal/config/projects.go. Most settings are shared by every watched
// project; each project's own entry in `list` carries its coding-CLI order.
// The add-project checks come from project_check, the coding-CLI probe from
// project_cli_list and clones from project_clone_list; their responses are
// normalized here so the views never depend on the raw payload shape.

export type Delivery = "poll" | "webhook";

/** One watched project in `projects.list`. */
export interface ProjectEntry {
	target: string;
	agents?: string[];
	// Other per-project settings (such as reports) pass through untouched.
	[key: string]: unknown;
}

/** The `projects:` section as stored in aviary.yaml (snake_case, sparse). */
export interface ProjectsSection {
	list?: ProjectEntry[];
	concurrency?: number;
	interval?: string;
	delivery?: string;
	ready_label?: string;
	ready_state?: string;
	filter?: string[];
	all_issues?: boolean;
	allowed_commenters?: string[];
	agents?: string[];
	no_merge?: boolean;
	no_changelog?: boolean;
	yolo?: boolean;
	unblock_interval?: string;
	temp_dirs?: string[];
	// Fields this page doesn't edit (agent_binaries, cli_definitions,
	// webhook, unblock_agent, ...) pass through untouched.
	[key: string]: unknown;
}

/** The settings edited on one project's page, with defaults filled in. */
export interface ProjectSettings {
	target: string;
	/** This project's coding-CLI order; empty uses the shared `agents`. */
	agents: string[];
	concurrency: number;
	readyLabel: string;
	readyState: string;
	delivery: Delivery;
	interval: string;
	filter: string[];
	allIssues: boolean;
	commenters: string[];
	merge: boolean;
	changelog: boolean;
	yolo: boolean;
	unblockInterval: string;
	tempDirs: string[];
}

export const DEFAULT_CONCURRENCY = 3;
export const DEFAULT_READY_LABEL = "ready";
export const DEFAULT_INTERVAL = "30s";
export const DEFAULT_FILTER = ["assignee:@me author:@me"];
export const DEFAULT_AGENT = "codex";

export const DELIVERY_OPTIONS: { key: Delivery; label: string }[] = [
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

/** The shared coding-CLI specs a project uses when it sets none of its own. */
export function sharedAgents(section: ProjectsSection | undefined): string[] {
	const agents = strings(section?.agents);
	return agents.length ? agents : [DEFAULT_AGENT];
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
		agents: strings(entry?.agents),
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
		delivery:
			(s.delivery ?? "").trim().toLowerCase() === "webhook"
				? "webhook"
				: "poll",
		interval: (s.interval ?? "").trim(),
		filter: filter.length ? filter : [...DEFAULT_FILTER],
		allIssues: s.all_issues === true,
		commenters: strings(s.allowed_commenters),
		merge: s.no_merge !== true,
		changelog: s.no_changelog !== true,
		yolo: s.yolo === true,
		unblockInterval: (s.unblock_interval ?? "").trim(),
		tempDirs: strings(s.temp_dirs),
	};
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
	set("delivery", s.delivery, s.delivery === "poll");
	const interval = s.interval.trim();
	set("interval", interval, !interval || interval === DEFAULT_INTERVAL);
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

	const agents = clean(s.agents);
	const current = section?.list ?? [];
	const at = current.findIndex((p) => sameTarget(p.target, s.target));
	const list = current.filter((p) => !sameTarget(p.target, s.target));
	// Keep the entry's other fields (such as reports); only agents change here.
	const { agents: _, ...rest } = at >= 0 ? current[at] : { target: s.target };
	const extra = Object.keys(rest).some((k) => k !== "target");
	if (listed || agents.length || extra) {
		const entry: ProjectEntry = {
			...rest,
			target: s.target.trim(),
			...(agents.length ? { agents } : {}),
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
	clis: "Coding CLIs",
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

// ── Coding-CLI probe (project_cli_list) ───────────────────────────────────

export interface CodingCLI {
	name: string;
	installed: boolean;
	signedIn: boolean;
	auth: string;
	quota: string;
	/** Fully qualified model specs, such as claude/opus. */
	models: string[];
	defaultModel: string;
}

// The agent doctor reports auth as "signed in", "signed out" or "unknown".

export function normalizeCLIList(raw: unknown): CodingCLI[] {
	const r = obj(raw);
	const list = Array.isArray(raw) ? raw : Array.isArray(r.clis) ? r.clis : [];
	return list
		.map((c) => {
			const o = obj(c);
			const name = str(o.name);
			const auth = str(o.auth);
			const installed = o.installed !== false && str(o.status) !== "missing";
			return {
				name,
				installed,
				signedIn: installed && (o.signed_in === true || auth === "signed in"),
				auth,
				quota: str(o.quota),
				models: strings(o.models).map((m) =>
					m.includes("/") ? m : `${name}/${m}`,
				),
				defaultModel: str(o.defaultModel ?? o.default_model),
			};
		})
		.filter((c) => c.name);
}

/** The agent name of a coding-CLI spec (AGENT/MODEL:LEVEL or aviary/<agent>). */
export function specName(spec: string): string {
	const name = spec.split("/")[0] ?? spec;
	return name.split(":")[0] ?? name;
}

/** The model a spec pins, as a fully qualified spec without the level. */
export function specModel(spec: string): string {
	if (specName(spec) === "aviary" || !spec.includes("/")) return "";
	return spec.split(":")[0] ?? "";
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
