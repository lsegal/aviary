// View models for the Projects board and run view, normalized from the
// project_* MCP tools (project_status, project_job_list, project_job_get)
// served by internal/projects. Every tool response passes through the
// normalizers here so the views never depend on the raw payload shape.

/** Job states as internal/projects/core reports them. */
export type JobStatus =
	| "queued"
	| "active"
	| "waiting"
	| "paused"
	| "stopping"
	| "complete"
	| "failed";

export type BoardFilter = "all" | "running" | "waiting" | "done" | "failed";

export interface ProjectSummary {
	name: string;
	running: number;
	waiting: number;
	issues: number;
}

export interface CLIStatus {
	name: string;
	quota: string;
	signedIn: boolean;
}

export interface ProjectTarget {
	name: string;
	issues: number;
}

export interface ProjectStatus {
	instanceID: string;
	running: boolean;
	error: string;
	delivery: string;
	deliveryOnline: boolean;
	lastChecked: string;
	interval: string;
	readyLabel: string;
	/** GitHub logins whose comments may steer jobs. */
	allowedCommenters: string[];
	slotsUsed: number;
	slotsTotal: number;
	activeJobs: number;
	waitingJobs: number;
	merged24h: number;
	failed: number;
	targets: ProjectTarget[];
	clis: CLIStatus[];
	log: string[];
}

export interface JobActions {
	retry: boolean;
	stop: boolean;
	retryUnavailable: string;
}

export interface ProjectJob {
	/** The engine's job key, `<target>#<number>`. */
	id: string;
	/** The watched target the job belongs to, which is also its project. */
	target: string;
	/** The OWNER/REPO hosting the issue. */
	repo: string;
	number: number;
	title: string;
	status: JobStatus;
	skill: string;
	cli: string;
	issueURL: string;
	pr: number;
	prURL: string;
	/** The OWNER/REPO hosting the pull request, which differs for forks. */
	prRepo: string;
	waitingReason: string;
	wakeAt: string;
	progressDone: number;
	progressTotal: number;
	startedAt: string;
	cloneDir: string;
	session: string;
	actions: JobActions;
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
			body: string;
			url: string;
	  }
	| { kind: "system"; time: string; text: string };

export interface JobDetail {
	job: ProjectJob;
	events: JobEvent[];
	/** Raw CLI stdout, falling back to the job's scrollback. */
	output: string[];
}

type Raw = Record<string, unknown>;

function obj(v: unknown): Raw {
	return v && typeof v === "object" && !Array.isArray(v) ? (v as Raw) : {};
}

function arr(v: unknown): unknown[] {
	return Array.isArray(v) ? v : [];
}

function str(v: unknown): string {
	if (typeof v === "string") return v;
	if (typeof v === "number") return String(v);
	return "";
}

function num(v: unknown): number {
	return typeof v === "number" && Number.isFinite(v) ? v : 0;
}

function lines(v: unknown): string[] {
	if (typeof v === "string") {
		const text = v.replaceAll("\r\n", "\n").replace(/\n$/, "");
		return text === "" ? [] : text.split("\n");
	}
	return arr(v).map(str);
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

const STATUSES: JobStatus[] = [
	"queued",
	"active",
	"waiting",
	"paused",
	"stopping",
	"complete",
	"failed",
];

export function normalizeJobStatus(v: unknown): JobStatus {
	const s = (str(v) === "completed" ? "complete" : str(v)) as JobStatus;
	return STATUSES.includes(s) ? s : "queued";
}

const SKILLS: Record<string, string> = {
	issue: "gh-fix",
	discussion: "gh-discuss",
	schedule: "schedule",
};

/** The OWNER/REPO of a github.com issue or pull request URL. */
function repoOf(url: string): string {
	const m = /^https:\/\/github\.com\/([^/]+\/[^/]+)\//.exec(url);
	return m ? m[1] : "";
}

export function normalizeJob(v: unknown): ProjectJob {
	const raw = obj(v);
	const progress = obj(raw.progress);
	const actions = obj(raw.actions);
	const target = str(raw.target);
	const number = num(raw.number);
	const issueURL = str(raw.issue_url);
	const repo = str(raw.repository) || repoOf(issueURL) || target;
	const prURL = str(raw.pull_request_url);
	const agent = str(raw.agent);
	const model = str(raw.model);
	return {
		id: str(raw.key) || `${target}#${number}`,
		target,
		repo,
		number,
		title: str(raw.title),
		status: normalizeJobStatus(raw.status),
		skill: SKILLS[str(raw.kind)] ?? str(raw.kind),
		cli: model ? `${agent}/${model}` : agent,
		issueURL,
		pr: num(raw.pull_request),
		prURL,
		prRepo: repoOf(prURL) || repo,
		waitingReason: str(raw.wait_reason),
		wakeAt: str(raw.wake_at),
		progressDone: num(progress.done),
		progressTotal: num(progress.total),
		startedAt: str(raw.started),
		cloneDir: str(raw.checkout_directory),
		session: str(raw.session_id),
		actions: {
			retry: actions.retry !== false,
			stop: actions.stop === true,
			retryUnavailable: str(raw.retry_unavailable),
		},
		output: lines(raw.scrollback),
	};
}

export function normalizeJobList(v: unknown): {
	jobs: ProjectJob[];
	total: number;
} {
	const raw = obj(v);
	const jobs = arr(raw.jobs).map(normalizeJob);
	return { jobs, total: num(raw.total) || jobs.length };
}

function normalizeEvent(v: unknown): JobEvent | null {
	const raw = obj(v);
	const time = str(raw.time);
	switch (str(raw.kind)) {
		case "message":
			return { kind: "message", time, text: str(raw.text) };
		case "reasoning":
			return { kind: "reasoning", time, text: str(raw.text) };
		case "tool_call":
			return {
				kind: "tool_call",
				time,
				callID: str(raw.call_id),
				name: str(raw.name),
				input: raw.input,
			};
		case "command":
			return {
				kind: "command",
				time,
				command: str(raw.command),
				output: str(raw.output),
				exitCode: typeof raw.exit_code === "number" ? raw.exit_code : null,
				running: str(raw.status) === "running",
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
						text: str(item.text),
						status: str(item.status) || "pending",
					};
				}),
			};
		case "usage": {
			const usage = obj(raw.usage);
			return {
				kind: "usage",
				time,
				inputTokens: num(usage.input_tokens),
				outputTokens: num(usage.output_tokens),
				cost: num(usage.cost_usd),
			};
		}
		case "comment_relay":
			return {
				kind: "comment_relay",
				time,
				author: str(raw.author),
				body: str(raw.text),
				url: str(raw.url),
			};
		case "system":
			return { kind: "system", time, text: str(raw.text) };
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
		if (str(raw.kind) === "tool_result") {
			const call = calls.get(str(raw.call_id));
			if (call) {
				call.result = {
					output: str(raw.output),
					error: raw.is_error === true,
					images: arr(raw.images).map(str),
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
	const job = normalizeJob(raw);
	const rawOutput = lines(raw.raw_output);
	return {
		job,
		events: normalizeEvents(raw.events),
		output: rawOutput.length > 0 ? rawOutput : job.output,
	};
}

export function normalizeStatus(v: unknown): ProjectStatus {
	const raw = obj(v);
	const jobs = obj(raw.jobs);
	const counts = obj(raw.issue_counts);
	const quotas = obj(raw.quotas);
	const cliNames = [
		...new Set([...arr(raw.agents).map(str), ...Object.keys(quotas)]),
	].filter(Boolean);
	return {
		instanceID: str(raw.identity),
		running: raw.running !== false,
		error: str(raw.error),
		delivery: str(raw.delivery),
		// Only webhook delivery reports whether its tunnel is up.
		deliveryOnline: raw.webhook_online !== false,
		lastChecked: str(raw.last_check),
		interval: str(raw.poll_interval),
		readyLabel: str(raw.ready_label) || "ready",
		allowedCommenters: arr(raw.allowed_commenters).map(str).filter(Boolean),
		slotsUsed: num(jobs.running),
		slotsTotal: num(raw.concurrency),
		activeJobs: num(jobs.running),
		waitingJobs: num(jobs.queued) + num(jobs.paused),
		merged24h: num(jobs.merged_24h),
		failed: num(jobs.failed),
		targets: arr(raw.targets).map((t) => ({
			name: str(t),
			issues: num(counts[str(t)]),
		})),
		clis: cliNames.map((name) => {
			const quota = str(quotas[name]);
			return {
				name,
				quota,
				signedIn: !/signed out|not signed in|unauthenticated/i.test(quota),
			};
		}),
		log: lines(raw.recent_logs),
	};
}

/** Per-target live counts for the sidebar, from status plus the job list. */
export function summarizeProjects(
	status: ProjectStatus,
	jobs: ProjectJob[],
): ProjectSummary[] {
	return status.targets.map((t) => {
		const mine = jobs.filter((j) => j.target === t.name);
		return {
			name: t.name,
			issues: t.issues,
			running: mine.filter((j) => isRunning(j.status)).length,
			waiting: mine.filter((j) => isWaiting(j.status)).length,
		};
	});
}

export function isRunning(status: JobStatus): boolean {
	return status === "active" || status === "stopping";
}

export function isWaiting(status: JobStatus): boolean {
	return status === "waiting" || status === "paused" || status === "queued";
}

const STATUS_ORDER: Record<JobStatus, number> = {
	active: 0,
	stopping: 0,
	failed: 1,
	waiting: 2,
	paused: 3,
	queued: 4,
	complete: 5,
};

/** Orders jobs with running and errored jobs first, then newest first. */
export function sortJobs(jobs: ProjectJob[]): ProjectJob[] {
	return [...jobs].sort(
		(a, b) =>
			STATUS_ORDER[a.status] - STATUS_ORDER[b.status] ||
			b.startedAt.localeCompare(a.startedAt),
	);
}

/** The engine statuses a board tab shows, or undefined for every job. */
export function filterStatuses(filter: BoardFilter): JobStatus[] | undefined {
	switch (filter) {
		case "running":
			return ["active", "stopping"];
		case "waiting":
			return ["waiting", "paused", "queued"];
		case "done":
			return ["complete"];
		case "failed":
			return ["failed"];
		default:
			return undefined;
	}
}

const STATUS_LABELS: Record<JobStatus, string> = {
	queued: "Queued",
	active: "Running",
	waiting: "Waiting",
	paused: "Paused",
	stopping: "Stopping",
	complete: "Done",
	failed: "Failed",
};

export function statusLabel(status: JobStatus): string {
	return STATUS_LABELS[status];
}

export function statusTone(status: JobStatus): string {
	if (isRunning(status)) return "blue";
	if (isWaiting(status)) return "amber";
	if (status === "failed") return "red";
	if (status === "complete") return "green";
	return "gray";
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
	if (job.status === "complete" && job.pr > 0) return { step: 5, done: true };
	let step = job.pr > 0 ? 1 : 0;
	for (const e of events) {
		if (e.kind === "system") {
			const t = e.text.toLowerCase();
			if (t.includes("merged")) return { step: 5, done: true };
			if (/\bci\b|checks?\b/.test(t)) step = Math.max(step, 4);
			else if (t.includes("ready")) step = Math.max(step, 3);
			else if (/\bpr\b|pull request|branch/.test(t)) step = Math.max(step, 1);
		} else if (step >= 1 && e.kind !== "comment_relay") {
			step = Math.max(step, 2);
		}
	}
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

export function jobPath(target: string, number: number): string {
	return `/projects/${encodeProject(target)}/jobs/${number}`;
}
