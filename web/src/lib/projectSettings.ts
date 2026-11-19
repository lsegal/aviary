// Project settings and the add-project flow. A project's settings live in
// the `projects:` section of aviary.yaml (read and written with config_get /
// config_save), and each entry holds only the values that differ from the
// defaults below, which mirror internal/config/projects.go. The add-project
// checks come from project_check, the coding-CLI probe from project_cli_list
// and clones from project_clone_list; their responses are normalized here so
// the views never depend on the raw payload shape.

export type Delivery = "browser" | "webhook" | "poll";
export type ReportEvent = "ready" | "failed" | "answer_needed" | "merged";

/** One `projects:` entry as stored in aviary.yaml (snake_case, sparse). */
export interface ProjectConfigEntry {
	target: string;
	jobs?: number;
	ready_label?: string;
	ready_state?: string;
	delivery?: Delivery;
	interval?: string;
	filters?: string[];
	all_issues?: boolean;
	commenters?: string[];
	merge?: boolean;
	changelog?: boolean;
	stack?: boolean;
	clis?: string[];
	temp_dirs?: string[];
	report_to?: string;
	report_on?: ReportEvent[];
	schedules?: ProjectSchedule[];
}

export interface ProjectSchedule {
	schedule: string;
	prompt: string;
	paused?: boolean;
}

/** A project's full, effective settings as edited in the form. */
export interface ProjectSettings {
	target: string;
	jobs: number;
	readyLabel: string;
	readyState: string;
	delivery: Delivery;
	interval: string;
	filters: string[];
	allIssues: boolean;
	commenters: string[];
	merge: boolean;
	changelog: boolean;
	stack: boolean;
	clis: string[];
	tempDirs: string[];
	reportTo: string;
	reportOn: ReportEvent[];
	schedules: ProjectSchedule[];
}

export const DEFAULT_FILTERS = ["assignee:@me author:@me"];
export const DEFAULT_REPORT_ON: ReportEvent[] = [
	"ready",
	"failed",
	"answer_needed",
];

export const REPORT_EVENTS: { key: ReportEvent; label: string }[] = [
	{ key: "ready", label: "A pull request is ready for review" },
	{ key: "failed", label: "A job fails" },
	{ key: "merged", label: "A pull request merges" },
	{ key: "answer_needed", label: "A job needs an answer from you" },
];

export const DELIVERY_OPTIONS: { key: Delivery; label: string }[] = [
	{ key: "browser", label: "Browser (signed-in profile)" },
	{ key: "webhook", label: "Webhook (ngrok tunnel)" },
	{ key: "poll", label: "Poll with gh" },
];

export function defaultSettings(target: string): ProjectSettings {
	return {
		target,
		jobs: 4,
		readyLabel: "ready",
		readyState: "",
		delivery: "browser",
		interval: "",
		filters: [...DEFAULT_FILTERS],
		allIssues: false,
		commenters: [],
		merge: true,
		changelog: true,
		stack: true,
		clis: [],
		tempDirs: [],
		reportTo: "",
		reportOn: [...DEFAULT_REPORT_ON],
		schedules: [],
	};
}

const DELIVERIES: Delivery[] = ["browser", "webhook", "poll"];

function strings(v: unknown): string[] {
	return Array.isArray(v)
		? v.filter((s): s is string => typeof s === "string" && s.trim() !== "")
		: [];
}

/** Finds target's entry, matching case-insensitively like the server. */
export function findEntry(
	projects: ProjectConfigEntry[] | undefined,
	target: string,
): ProjectConfigEntry | undefined {
	const key = target.toLowerCase();
	return (projects ?? []).find((p) => p.target?.toLowerCase() === key);
}

/** Fills a sparse config entry with defaults to get the form's settings. */
export function settingsFromEntry(
	target: string,
	entry?: ProjectConfigEntry,
): ProjectSettings {
	const s = defaultSettings(entry?.target || target);
	if (!entry) return s;
	if (typeof entry.jobs === "number" && entry.jobs > 0) s.jobs = entry.jobs;
	if (entry.ready_label) s.readyLabel = entry.ready_label;
	if (entry.ready_state) s.readyState = entry.ready_state;
	if (entry.delivery && DELIVERIES.includes(entry.delivery))
		s.delivery = entry.delivery;
	if (entry.interval) s.interval = entry.interval;
	s.allIssues = entry.all_issues === true;
	const filters = strings(entry.filters);
	if (filters.length) s.filters = filters;
	else if (s.allIssues) s.filters = [];
	s.commenters = strings(entry.commenters);
	s.merge = entry.merge !== false;
	s.changelog = entry.changelog !== false;
	s.stack = entry.stack !== false;
	s.clis = strings(entry.clis);
	s.tempDirs = strings(entry.temp_dirs);
	s.reportTo = entry.report_to ?? "";
	const on = strings(entry.report_on) as ReportEvent[];
	if (on.length) s.reportOn = on;
	s.schedules = (entry.schedules ?? [])
		.filter((sc) => sc && typeof sc === "object")
		.map((sc) => ({
			schedule: sc.schedule ?? "",
			prompt: sc.prompt ?? "",
			paused: sc.paused === true,
		}));
	return s;
}

function sameList(a: string[], b: string[]): boolean {
	return a.length === b.length && a.every((v, i) => v === b[i]);
}

function clean(list: string[]): string[] {
	return list.map((v) => v.trim()).filter(Boolean);
}

/**
 * Builds the sparse entry that records only what differs from the defaults,
 * or null when every setting is a default and nothing should be written.
 */
export function entryFromSettings(
	s: ProjectSettings,
): ProjectConfigEntry | null {
	const d = defaultSettings(s.target);
	const e: ProjectConfigEntry = { target: s.target.trim() };
	if (s.jobs > 0 && s.jobs !== d.jobs) e.jobs = Math.floor(s.jobs);
	const label = s.readyLabel.trim();
	if (label && label !== d.readyLabel) e.ready_label = label;
	if (s.readyState.trim()) e.ready_state = s.readyState.trim();
	if (s.delivery !== d.delivery) e.delivery = s.delivery;
	if (s.interval.trim()) e.interval = s.interval.trim();
	if (s.allIssues) e.all_issues = true;
	const filters = clean(s.filters);
	if (filters.length && !sameList(filters, d.filters)) e.filters = filters;
	const commenters = clean(s.commenters);
	if (commenters.length) e.commenters = commenters;
	if (!s.merge) e.merge = false;
	if (!s.changelog) e.changelog = false;
	if (!s.stack) e.stack = false;
	const clis = clean(s.clis);
	if (clis.length) e.clis = clis;
	const dirs = clean(s.tempDirs);
	if (dirs.length) e.temp_dirs = dirs;
	if (s.reportTo.trim()) {
		e.report_to = s.reportTo.trim();
		const on = REPORT_EVENTS.map((r) => r.key).filter((k) =>
			s.reportOn.includes(k),
		);
		if (!sameList(on, DEFAULT_REPORT_ON)) e.report_on = on;
	}
	const schedules = s.schedules
		.filter((sc) => sc.schedule.trim() || sc.prompt.trim())
		.map((sc) => ({
			schedule: sc.schedule.trim(),
			prompt: sc.prompt.trim(),
			...(sc.paused ? { paused: true } : {}),
		}));
	if (schedules.length) e.schedules = schedules;
	return Object.keys(e).length > 1 ? e : null;
}

/** Returns projects with target's entry replaced, added or (when null) removed. */
export function withEntry(
	projects: ProjectConfigEntry[] | undefined,
	target: string,
	entry: ProjectConfigEntry | null,
): ProjectConfigEntry[] {
	const key = target.toLowerCase();
	const out: ProjectConfigEntry[] = [];
	let placed = false;
	for (const p of projects ?? []) {
		if (p.target?.toLowerCase() === key) {
			if (entry && !placed) out.push(entry);
			placed = true;
		} else {
			out.push(p);
		}
	}
	if (entry && !placed) out.push(entry);
	return out;
}

/** Which form fields still hold their defaults, for the grey "default" tags. */
export function defaultFields(s: ProjectSettings): Set<keyof ProjectSettings> {
	const d = defaultSettings(s.target);
	const out = new Set<keyof ProjectSettings>();
	for (const key of Object.keys(d) as (keyof ProjectSettings)[]) {
		const a = s[key];
		const b = d[key];
		const same =
			Array.isArray(a) && Array.isArray(b)
				? JSON.stringify(a) === JSON.stringify(b)
				: a === b;
		if (same) out.add(key);
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

function yamlScalar(v: string | number | boolean): string {
	if (typeof v !== "string") return String(v);
	if (
		v === "" ||
		/^[\s]|[\s]$|[:#{}[\],&*!|>'"%@`]|^(true|false|null|yes|no|~|[-+]?\d)/i.test(
			v,
		)
	)
		return JSON.stringify(v);
	return v;
}

/** Renders the `projects:` section exactly as it will be written. */
export function projectsYAML(projects: ProjectConfigEntry[]): string {
	if (!projects.length) return "";
	const lines = ["projects:"];
	for (const p of projects) {
		let first = true;
		for (const [key, value] of Object.entries(p)) {
			if (value === undefined) continue;
			const lead = first ? "  - " : "    ";
			first = false;
			if (key === "schedules" && Array.isArray(value)) {
				lines.push(`${lead}schedules:`);
				for (const sc of value as ProjectSchedule[]) {
					lines.push(`      - schedule: ${yamlScalar(sc.schedule)}`);
					lines.push(`        prompt: ${yamlScalar(sc.prompt)}`);
					if (sc.paused) lines.push("        paused: true");
				}
			} else if (Array.isArray(value)) {
				lines.push(
					`${lead}${key}: [${value.map((v) => yamlScalar(String(v))).join(", ")}]`,
				);
			} else {
				lines.push(`${lead}${key}: ${yamlScalar(value as string)}`);
			}
		}
	}
	return lines.join("\n");
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
	readyLabel: string;
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
		readyLabel: str(r.ready_label),
		fork: r.fork === true,
		ok: r.ok === true && !checks.some((c) => c.status === "fail"),
		checks,
	};
}

// ── Coding-CLI probe (project_cli_list) ───────────────────────────────────

export interface CodingCLI {
	name: string;
	installed: boolean;
	version: string;
	signedIn: boolean;
	quota: string;
	models: string[];
	defaultModel: string;
}

export function normalizeCLIList(raw: unknown): CodingCLI[] {
	const r = obj(raw);
	const list = Array.isArray(raw)
		? raw
		: Array.isArray(r.clis)
			? r.clis
			: Array.isArray(r.agents)
				? r.agents
				: [];
	return list
		.map((c) => {
			const o = obj(c);
			const models = strings(o.models);
			return {
				name: str(o.name),
				installed: o.installed !== false,
				version: str(o.version),
				signedIn: o.signed_in === true || o.signedIn === true,
				quota: str(o.quota),
				models,
				defaultModel: str(o.default_model ?? o.defaultModel),
			};
		})
		.filter((c) => c.name);
}

/** A CLI fallback-order spec: `name`, `name/model`, or `aviary/<agent>`. */
export function specName(spec: string): string {
	return spec.split("/")[0]?.split(":")[0] ?? spec;
}

export function specModel(spec: string): string {
	const [name, ...rest] = spec.split("/");
	if (name === "aviary") return "";
	return rest.join("/").split(":")[0] ?? "";
}

// ── Clones (project_clone_list) ───────────────────────────────────────────

export interface ProjectClone {
	path: string;
	repo: string;
	issue: number;
	pr: number;
	sizeBytes: number;
	inUse: boolean;
}

export function normalizeClones(raw: unknown): {
	clones: ProjectClone[];
	totalBytes: number;
} {
	const r = obj(raw);
	const list = Array.isArray(raw)
		? raw
		: Array.isArray(r.clones)
			? r.clones
			: [];
	const clones = list
		.map((c) => {
			const o = obj(c);
			return {
				path: str(o.path ?? o.dir),
				repo: str(o.repo),
				issue: Number(o.issue) || 0,
				pr: Number(o.pr) || 0,
				sizeBytes: Number(o.size_bytes ?? o.size) || 0,
				inUse: o.in_use === true || o.active === true,
			};
		})
		.filter((c) => c.path);
	const total =
		Number(r.total_bytes) || clones.reduce((n, c) => n + c.sizeBytes, 0);
	return { clones, totalBytes: total };
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
