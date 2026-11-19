// View models for the Projects board and run view, normalized from the
// project_* MCP tools (project_status, project_job_list, project_job_get).
// Every tool response passes through the normalizers here so the views
// never depend on the raw payload shape.

export type JobStatus =
	| "queued"
	| "running"
	| "waiting"
	| "paused"
	| "completed"
	| "merged"
	| "failed"
	| "stopped";

export type BoardFilter = "all" | "running" | "waiting" | "done" | "failed";

export interface ProjectSummary {
	name: string;
	running: number;
	waiting: number;
	jobs: number;
}

export interface CLIStatus {
	name: string;
	model: string;
	signedIn: boolean;
	quota: string;
}

export interface ProjectTarget {
	name: string;
	issues: number;
}

export interface ProjectStatus {
	instanceID: string;
	version: string;
	deliveryMode: string;
	deliveryOnline: boolean;
	lastChecked: string;
	intervalSeconds: number;
	readyLabel: string;
	slotsUsed: number;
	slotsTotal: number;
	running: number;
	waiting: number;
	merged24h: number;
	failed: number;
	targets: ProjectTarget[];
	clis: CLIStatus[];
	log: string[];
	projects: ProjectSummary[];
}

export interface ProjectJob {
	id: string;
	project: string;
	repo: string;
	issue: number;
	title: string;
	status: JobStatus;
	skill: string;
	cli: string;
	branch: string;
	pr: number;
	prRepo: string;
	prDraft: boolean;
	stackedOn: number;
	waitingReason: string;
	checks: string;
	progressDone: number;
	progressTotal: number;
	startedAt: string;
	updatedAt: string;
	cloneDir: string;
	session: string;
	allowedCommenters: string[];
	output: string[];
}

export type JobEvent =
	| { kind: "message"; time: string; text: string }
	| { kind: "reasoning"; time: string; text: string }
	| {
			kind: "tool_call";
			time: string;
			callID: string;
			name: string;
			input: unknown;
			result?: { output: string; error: boolean; images: string[] };
	  }
	| {
			kind: "command";
			time: string;
			command: string;
			output: string;
			exitCode: number | null;
			running: boolean;
	  }
	| { kind: "file_change"; time: string; path: string; diff: string }
	| {
			kind: "plan";
			time: string;
			items: { text: string; status: string }[];
	  }
	| {
			kind: "usage";
			time: string;
			inputTokens: number;
			outputTokens: number;
			cost: number;
	  }
	| {
			kind: "comment_relay";
			time: string;
			author: string;
			target: string;
			body: string;
			url: string;
	  }
	| { kind: "system"; time: string; text: string };

export interface JobDetail {
	job: ProjectJob;
	events: JobEvent[];
	output: string[];
}

type Raw = Record<string, unknown>;

function obj(v: unknown): Raw {
	return v && typeof v === "object" && !Array.isArray(v) ? (v as Raw) : {};
}

function arr(v: unknown): unknown[] {
	return Array.isArray(v) ? v : [];
}

function str(...vals: unknown[]): string {
	for (const v of vals) {
		if (typeof v === "string" && v !== "") return v;
		if (typeof v === "number") return String(v);
	}
	return "";
}

function num(...vals: unknown[]): number {
	for (const v of vals) {
		if (typeof v === "number" && Number.isFinite(v)) return v;
		if (typeof v === "string" && v.trim() !== "" && !Number.isNaN(Number(v)))
			return Number(v);
	}
	return 0;
}

function bool(...vals: unknown[]): boolean {
	for (const v of vals) {
		if (typeof v === "boolean") return v;
	}
	return false;
}

function lines(v: unknown): string[] {
	if (typeof v === "string") return v === "" ? [] : v.split("\n");
	return arr(v).map((l) => (typeof l === "string" ? l : str(obj(l).text)));
}

/** Parses a tool's text result as JSON, returning null for empty or invalid text. */
export function parseToolJSON(text: string): unknown {
	if (!text.trim()) return null;
	try {
		return JSON.parse(text);
	} catch {
		return null;
	}
}

const STATUS_ALIASES: Record<string, JobStatus> = {
	queued: "queued",
	pending: "queued",
	running: "running",
	active: "running",
	waiting: "waiting",
	blocked: "waiting",
	paused: "paused",
	completed: "completed",
	done: "completed",
	succeeded: "completed",
	merged: "merged",
	failed: "failed",
	error: "failed",
	errored: "failed",
	stopped: "stopped",
	cancelled: "stopped",
	canceled: "stopped",
};

export function normalizeJobStatus(v: unknown): JobStatus {
	return STATUS_ALIASES[str(v).toLowerCase()] ?? "queued";
}

function normalizeProgress(raw: Raw): { done: number; total: number } {
	const p = obj(raw.progress);
	if (Object.keys(p).length > 0) {
		return { done: num(p.done, p.checked), total: num(p.total) };
	}
	return { done: num(raw.tasks_done), total: num(raw.tasks_total) };
}

export function normalizeJob(v: unknown): ProjectJob {
	const raw = obj(v);
	const pr = obj(raw.pr);
	const progress = normalizeProgress(raw);
	const repo = str(raw.repo, raw.repository);
	return {
		id: str(raw.id, raw.job_id),
		project: str(raw.project, repo),
		repo,
		issue: num(raw.issue, raw.issue_number, raw.number),
		title: str(raw.title, raw.issue_title),
		status: normalizeJobStatus(raw.status ?? raw.state),
		skill: str(raw.skill),
		cli: str(raw.cli, raw.agent),
		branch: str(raw.branch, pr.branch),
		pr: num(pr.number, raw.pr_number, typeof raw.pr === "number" ? raw.pr : 0),
		prRepo: str(pr.repo, raw.pr_repo, repo),
		prDraft: bool(pr.draft, raw.pr_draft),
		stackedOn: num(raw.stacked_on, pr.stacked_on),
		waitingReason: str(raw.waiting_reason, raw.reason, raw.blocked_reason),
		checks: str(raw.checks, raw.ci_summary),
		progressDone: progress.done,
		progressTotal: progress.total,
		startedAt: str(raw.started_at, raw.created_at),
		updatedAt: str(raw.updated_at),
		cloneDir: str(raw.clone_dir, raw.checkout_dir),
		session: str(raw.session, raw.session_id),
		allowedCommenters: arr(raw.allowed_commenters).map((c) => str(c)),
		output: lines(raw.output ?? raw.scrollback ?? raw.output_tail),
	};
}

export function normalizeJobList(v: unknown): {
	jobs: ProjectJob[];
	total: number;
} {
	const raw = obj(v);
	const list = Array.isArray(v) ? v : arr(raw.jobs);
	const jobs = list.map(normalizeJob);
	return { jobs, total: num(raw.total) || jobs.length };
}

function normalizeEvent(v: unknown): JobEvent | null {
	const raw = obj(v);
	const time = str(raw.time, raw.timestamp, raw.at);
	switch (str(raw.type, raw.kind)) {
		case "message":
			return { kind: "message", time, text: str(raw.text, raw.content) };
		case "reasoning":
			return { kind: "reasoning", time, text: str(raw.text, raw.content) };
		case "tool_call":
			return {
				kind: "tool_call",
				time,
				callID: str(raw.call_id, raw.id),
				name: str(raw.name, raw.tool),
				input: raw.input ?? raw.args,
			};
		case "tool_result":
			// Merged into its tool_call by normalizeEvents.
			return null;
		case "command":
			return {
				kind: "command",
				time,
				command: str(raw.command),
				output: str(raw.output),
				exitCode:
					typeof raw.exit_code === "number" ? (raw.exit_code as number) : null,
				running: str(raw.status) === "running" || bool(raw.running),
			};
		case "file_change":
			return {
				kind: "file_change",
				time,
				path: str(raw.path),
				diff: str(raw.diff),
			};
		case "plan":
			return {
				kind: "plan",
				time,
				items: arr(raw.items).map((i) => {
					const item = obj(i);
					return {
						text: str(item.text, item.content, item.step),
						status: str(item.status) || "pending",
					};
				}),
			};
		case "usage":
			return {
				kind: "usage",
				time,
				inputTokens: num(raw.input_tokens),
				outputTokens: num(raw.output_tokens),
				cost: num(raw.cost, raw.cost_usd),
			};
		case "comment_relay":
			return {
				kind: "comment_relay",
				time,
				author: str(raw.author),
				target: str(raw.target),
				body: str(raw.body, raw.text),
				url: str(raw.url),
			};
		case "system":
			return {
				kind: "system",
				time,
				text: str(raw.text, raw.message, raw.event),
			};
		default:
			return null;
	}
}

/** Normalizes typed job events, attaching each tool_result to its tool_call. */
export function normalizeEvents(v: unknown): JobEvent[] {
	const events: JobEvent[] = [];
	const calls = new Map<string, Extract<JobEvent, { kind: "tool_call" }>>();
	for (const item of arr(v)) {
		const raw = obj(item);
		if (str(raw.type, raw.kind) === "tool_result") {
			const call = calls.get(str(raw.call_id, raw.id));
			if (call) {
				call.result = {
					output: str(raw.output, raw.content),
					error: bool(raw.error, raw.is_error),
					images: arr(raw.images).map((i) => str(i, obj(i).url)),
				};
			}
			continue;
		}
		const event = normalizeEvent(item);
		if (!event) continue;
		if (event.kind === "tool_call" && event.callID)
			calls.set(event.callID, event);
		events.push(event);
	}
	return events;
}

export function normalizeJobDetail(v: unknown): JobDetail {
	const raw = obj(v);
	const jobRaw = Object.keys(obj(raw.job)).length > 0 ? obj(raw.job) : raw;
	const job = normalizeJob(jobRaw);
	const output = lines(raw.output ?? raw.scrollback ?? raw.raw_output);
	job.output = output.length > 0 ? output : job.output;
	return {
		job,
		events: normalizeEvents(raw.events ?? jobRaw.events),
		output: job.output,
	};
}

export function normalizeStatus(v: unknown): ProjectStatus {
	const raw = obj(v);
	const delivery = obj(raw.delivery);
	const slots = obj(raw.slots);
	const counts = obj(raw.counts ?? raw.jobs);
	return {
		instanceID: str(raw.instance_id, raw.identity),
		version: str(raw.version),
		deliveryMode: str(delivery.mode, raw.delivery_mode),
		deliveryOnline:
			typeof delivery.online === "boolean"
				? (delivery.online as boolean)
				: str(delivery.status, raw.delivery_status) !== "offline",
		lastChecked: str(raw.last_checked, delivery.last_checked),
		intervalSeconds: num(raw.interval_seconds, delivery.interval_seconds),
		readyLabel: str(raw.ready_label) || "ready",
		slotsUsed: num(slots.used, raw.slots_used),
		slotsTotal: num(slots.total, raw.slots_total, raw.concurrency),
		running: num(counts.running),
		waiting: num(counts.waiting),
		merged24h: num(counts.merged_24h, counts.merged),
		failed: num(counts.failed),
		targets: arr(raw.targets).map((t) => {
			const target = obj(t);
			return {
				name: str(target.name, target.target, t),
				issues: num(target.issues, target.issue_count),
			};
		}),
		clis: arr(raw.clis ?? raw.quota).map((c) => {
			const cli = obj(c);
			return {
				name: str(cli.name, cli.cli, cli.agent),
				model: str(cli.model),
				signedIn: bool(cli.signed_in, cli.authenticated),
				quota: str(cli.quota, cli.quota_summary),
			};
		}),
		log: lines(raw.log ?? raw.recent_log),
		projects: arr(raw.projects).map((p) => {
			const project = obj(p);
			return {
				name: str(project.name, project.project, p),
				running: num(project.running),
				waiting: num(project.waiting),
				jobs: num(project.jobs, project.total),
			};
		}),
	};
}

const STATUS_ORDER: Record<JobStatus, number> = {
	running: 0,
	failed: 1,
	waiting: 2,
	paused: 3,
	queued: 4,
	stopped: 5,
	completed: 6,
	merged: 7,
};

/** Orders jobs with running and errored jobs first, then newest first. */
export function sortJobs(jobs: ProjectJob[]): ProjectJob[] {
	return [...jobs].sort(
		(a, b) =>
			STATUS_ORDER[a.status] - STATUS_ORDER[b.status] ||
			(b.updatedAt || b.startedAt).localeCompare(a.updatedAt || a.startedAt),
	);
}

/** The project_job_list status filter for a board tab, or undefined for all. */
export function filterStatuses(filter: BoardFilter): JobStatus[] | undefined {
	switch (filter) {
		case "running":
			return ["running"];
		case "waiting":
			return ["waiting", "paused", "queued"];
		case "done":
			return ["completed", "merged", "stopped"];
		case "failed":
			return ["failed"];
		default:
			return undefined;
	}
}

export function statusLabel(status: JobStatus): string {
	return status.charAt(0).toUpperCase() + status.slice(1);
}

export function statusTone(status: JobStatus): string {
	switch (status) {
		case "running":
			return "blue";
		case "waiting":
		case "paused":
		case "queued":
			return "amber";
		case "failed":
			return "red";
		case "completed":
		case "merged":
			return "green";
		default:
			return "gray";
	}
}

export const LIFECYCLE_STEPS = [
	"Claimed",
	"Branch + draft PR",
	"Implementing",
	"Ready for review",
	"CI",
	"Merged",
] as const;

/**
 * The lifecycle step a job is on (0-based) and whether it finished, derived
 * from its state and the system events it has emitted.
 */
export function lifecycleStep(detail: JobDetail): {
	step: number;
	done: boolean;
} {
	const { job, events } = detail;
	if (job.status === "merged") return { step: 5, done: true };
	let step = 0;
	if (job.pr > 0 || job.branch) step = 1;
	for (const e of events) {
		if (e.kind === "system") {
			const t = e.text.toLowerCase();
			if (/\bci\b|checks?/.test(t)) step = Math.max(step, 4);
			else if (t.includes("ready")) step = Math.max(step, 3);
			else if (t.includes("pr") || t.includes("branch"))
				step = Math.max(step, 1);
		} else if (step >= 1 && e.kind !== "comment_relay") {
			step = Math.max(step, 2);
		}
	}
	if (job.pr > 0 && !job.prDraft) step = Math.max(step, 3);
	if (job.checks && step >= 3) step = 4;
	return { step, done: false };
}

export function githubURL(repo: string, kind: "issues" | "pull", n: number) {
	return `https://github.com/${repo}/${kind}/${n}`;
}

/** A compact "14m" / "3h" / "2d" age for an RFC 3339 timestamp. */
export function age(iso: string, now = Date.now()): string {
	const t = Date.parse(iso);
	if (Number.isNaN(t)) return "";
	const s = Math.max(0, Math.round((now - t) / 1000));
	if (s < 60) return `${s}s`;
	if (s < 3600) return `${Math.floor(s / 60)}m`;
	if (s < 86400) return `${Math.floor(s / 3600)}h`;
	return `${Math.floor(s / 86400)}d`;
}

/** Splits a unified diff into lines tagged by kind for coloring. */
export function diffLines(
	diff: string,
): { text: string; kind: "add" | "del" | "hunk" | "ctx" }[] {
	return diff.split("\n").map((text) => {
		if (text.startsWith("@@")) return { text, kind: "hunk" };
		if (text.startsWith("+") && !text.startsWith("+++"))
			return { text, kind: "add" };
		if (text.startsWith("-") && !text.startsWith("---"))
			return { text, kind: "del" };
		return { text, kind: "ctx" };
	});
}

export function diffStat(diff: string): { added: number; removed: number } {
	let added = 0;
	let removed = 0;
	for (const l of diffLines(diff)) {
		if (l.kind === "add") added++;
		else if (l.kind === "del") removed++;
	}
	return { added, removed };
}

export function encodeProject(name: string): string {
	return encodeURIComponent(name);
}
