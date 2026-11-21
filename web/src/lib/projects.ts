import { type ScheduleTrigger, TRIGGERS, type TriggerFields } from "./trigger";

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

/**
 * The five states the board shows a job in, as internal/projects/core's
 * BoardState maps every engine status onto them.
 */
export type BoardState = "starting" | "paused" | "running" | "done" | "failed";

export const BOARD_STATES: BoardState[] = [
	"starting",
	"paused",
	"running",
	"done",
	"failed",
];

/**
 * The board's filter chips: its scheduled items, then the five job states. A
 * scheduled item is an upcoming run, not a job; once it runs, its job shows
 * under its job state.
 */
export type BoardFilter = "scheduled" | BoardState;

export const BOARD_FILTERS: BoardFilter[] = ["scheduled", ...BOARD_STATES];

/**
 * The board filters a comma-separated list such as the board's ?state= query
 * names, in board order; unknown names are dropped and none means no filter.
 */
export function parseBoardStates(v: unknown): BoardFilter[] {
	const names = typeof v === "string" ? v.split(",") : [];
	return BOARD_FILTERS.filter((s) => names.includes(s));
}

/** The job states among the board filters. */
export function jobStates(filters: BoardFilter[]): BoardState[] {
	return filters.filter((s): s is BoardState => s !== "scheduled");
}

/** The state of a job's pull request, empty while it has none. */
export type PullRequestState = "" | "draft" | "open" | "merged";

export interface ProjectSummary {
	name: string;
	running: number;
	waiting: number;
	issues: number;
}

/** One CLI provider: the models the board's jobs run on and its quota. */
export interface CLIStatus {
	name: string;
	models: string[];
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
	/** The issue search that selects work; empty picks up every open issue. */
	issueFilter: string;
	/** GitHub logins whose comments may steer jobs. */
	allowedCommenters: string[];
	slotsUsed: number;
	slotsTotal: number;
	/** The jobs in each board state, counted as the board's filters count them. */
	states: Record<BoardState, number>;
	merged24h: number;
	targets: ProjectTarget[];
	clis: CLIStatus[];
	log: string[];
	/** Browser delivery's GitHub sign-in; null in other delivery modes. */
	github: GitHubSession | null;
}

/** What browser delivery knows about its GitHub sign-in. */
export interface GitHubSession {
	/** The last read was served to a signed-out session. */
	signedOut: boolean;
	/** A sign-in window is open in Aviary's browser now. */
	signingIn: boolean;
	/** The page that showed the signed-out session. */
	url: string;
	/** Its HTTP status when a 404 or 403 gave the session away. */
	httpStatus: number;
	since: string;
	/** Why no sign-in window can appear here, such as no display. */
	windowUnavailable: string;
	/** Why the last sign-in attempt did not work. */
	lastError: string;
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
	/** The board state the job shows in. */
	state: BoardState;
	skill: string;
	cli: string;
	issueURL: string;
	pr: number;
	prURL: string;
	/** The OWNER/REPO hosting the pull request, which differs for forks. */
	prRepo: string;
	prState: PullRequestState;
	waitingReason: string;
	wakeAt: string;
	progressDone: number;
	progressTotal: number;
	startedAt: string;
	cloneDir: string;
	/** Where the job's clone stands; only the job detail reports it. */
	cloneState: CloneState;
	/** The job that was handed this job's clone, when cloneState is "reused". */
	cloneReusedBy: number;
	session: string;
	actions: JobActions;
	output: string[];
	/** A scheduled prompt's run, which has no issue: its number is its fire time. */
	scheduled: boolean;
	/** The `agent/task` key of the schedule a scheduled run fired from. */
	scheduleID: string;
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
export function normalizeGitHubSession(v: unknown): GitHubSession | null {
	if (v === undefined || v === null) return null;
	const raw = obj(v);
	return {
		signedOut: raw.signed_out === true,
		signingIn: raw.signing_in === true,
		url: str(raw.url),
		httpStatus: num(raw.status),
		since: str(raw.since),
		windowUnavailable: str(raw.window_unavailable),
		lastError: str(raw.last_error),
	};
}

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

export function normalizeBoardState(v: unknown): BoardState {
	const s = str(v) as BoardState;
	return BOARD_STATES.includes(s) ? s : "starting";
}

const PR_STATES: PullRequestState[] = ["draft", "open", "merged"];

function normalizePullRequestState(v: unknown): PullRequestState {
	const s = str(v) as PullRequestState;
	return PR_STATES.includes(s) ? s : "";
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

/**
 * Where a job's clone stands: none assigned yet, on disk, handed to a later
 * job on the same repository, or deleted.
 */
export type CloneState = "none" | "present" | "reused" | "removed";

function normalizeCloneState(v: unknown, cloneDir: string): CloneState {
	if (v === "none" || v === "present" || v === "reused" || v === "removed")
		return v;
	return cloneDir ? "present" : "none";
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
		state: normalizeBoardState(raw.state),
		skill: SKILLS[str(raw.kind)] ?? str(raw.kind),
		cli: model ? `${agent}/${model}` : agent,
		issueURL,
		pr: num(raw.pull_request),
		prURL,
		prRepo: repoOf(prURL) || repo,
		prState: normalizePullRequestState(raw.pull_request_state),
		waitingReason: str(raw.wait_reason),
		wakeAt: str(raw.wake_at),
		progressDone: num(progress.done),
		progressTotal: num(progress.total),
		startedAt: str(raw.started),
		cloneDir: str(raw.checkout_directory),
		cloneState: normalizeCloneState(
			raw.checkout_state,
			str(raw.checkout_directory),
		),
		cloneReusedBy: num(raw.checkout_reused_by),
		session: str(raw.session_id),
		actions: {
			retry: actions.retry !== false,
			stop: actions.stop === true,
			retryUnavailable: str(raw.retry_unavailable),
		},
		output: lines(raw.scrollback),
		scheduled: str(raw.kind) === "schedule",
		scheduleID: str(raw.schedule_id),
	};
}

export function normalizeJobList(v: unknown): {
	jobs: ProjectJob[];
	total: number;
	/** The jobs before a search query narrowed them; total without one. */
	unfilteredTotal: number;
} {
	const raw = obj(v);
	const jobs = arr(raw.jobs).map(normalizeJob);
	const total = num(raw.total) || jobs.length;
	return {
		jobs,
		total,
		unfilteredTotal:
			raw.unfiltered_total === undefined ? total : num(raw.unfiltered_total),
	};
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
	const states = obj(raw.states);
	return {
		instanceID: str(raw.identity),
		running: raw.running !== false,
		error: str(raw.error),
		delivery: str(raw.delivery),
		// Only webhook delivery reports whether its tunnel is up.
		deliveryOnline: raw.webhook_online !== false,
		lastChecked: str(raw.last_check),
		interval: str(raw.poll_interval),
		issueFilter: str(raw.issue_filter),
		allowedCommenters: arr(raw.allowed_commenters).map(str).filter(Boolean),
		slotsUsed: num(jobs.running),
		slotsTotal: num(raw.concurrency),
		states: Object.fromEntries(
			BOARD_STATES.map((s) => [s, num(states[s])]),
		) as Record<BoardState, number>,
		merged24h: num(jobs.merged_24h),
		github: normalizeGitHubSession(raw.github),
		targets: arr(raw.targets).map((t) => ({
			name: str(t),
			issues: num(counts[str(t)]),
		})),
		clis: arr(raw.clis)
			.map(obj)
			.filter((c) => str(c.name))
			.map((c) => {
				const quota = str(c.quota);
				return {
					name: str(c.name),
					models: arr(c.models).map(str).filter(Boolean),
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

const STATE_LABELS: Record<BoardFilter, string> = {
	scheduled: "Scheduled",
	starting: "Starting",
	paused: "Paused",
	running: "Running",
	done: "Done",
	failed: "Failed",
};

export function stateLabel(state: BoardFilter): string {
	return STATE_LABELS[state];
}

const PR_STATE_LABELS: Record<Exclude<PullRequestState, "">, string> = {
	draft: "Draft pull request",
	open: "Open pull request",
	merged: "Merged pull request",
};

/** The hover text of a pull request's state icon. */
export function pullRequestStateLabel(state: PullRequestState): string {
	return state ? PR_STATE_LABELS[state] : "Pull request";
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
 * from its state and the system events it has emitted. The pull request's
 * state, which the board's PR icon shows, sets the lowest step it can be on.
 */
export function lifecycleStep(detail: JobDetail): {
	step: number;
	done: boolean;
} {
	const { job, events } = detail;
	if (job.prState === "merged") return { step: 5, done: true };
	if (job.status === "complete" && job.pr > 0) return { step: 5, done: true };
	let step = job.prState === "open" ? 4 : job.pr > 0 ? 1 : 0;
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

/** The prompt chat's request for a summary of a job's session log. */
export function summarizePrompt(job: Pick<ProjectJob, "repo" | "number">) {
	return `Summarize the current session log of work on ${job.repo}#${job.number}`;
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

/**
 * The GitHub page of a watch target: an OWNER/REPO target's repository, or
 * the repository, project board, or Discussions URL a target is written as.
 */
export function targetURL(target: string): string {
	return target.startsWith("https://")
		? target
		: `https://github.com/${target}`;
}

export function encodeProject(name: string): string {
	return encodeURIComponent(name);
}

/**
 * The board path of target: under its owning agent's GitHub Projects, or
 * under System → Watches for a runtime watch no agent owns.
 */
export function projectPath(owner: string, target: string): string {
	return owner
		? `/agents/${encodeURIComponent(owner)}/projects/${encodeProject(target)}`
		: `/system/watches/${encodeProject(target)}`;
}

export function jobPath(owner: string, target: string, number: number): string {
	return `${projectPath(owner, target)}/jobs/${number}`;
}

export interface ScheduleRun {
	fireTime: string;
	reason: string;
	status: string;
	summary: string;
	durationMS: number;
}

/** A project's scheduled prompt: an Aviary task of type `project`. */
export interface ProjectSchedule extends TriggerFields {
	/** The task's `agent/task` key. */
	id: string;
	agent: string;
	name: string;
	project: string;
	/** Overrides the project agent's model for the task's runs; empty uses the agent's own. */
	model: string;
	prompt: string;
	enabled: boolean;
	trigger: ScheduleTrigger;
	description: string;
	nextRun: string;
	/** Most recent first, at most 20. */
	history: ScheduleRun[];
}

function normalizeTrigger(raw: Record<string, unknown>): ScheduleTrigger {
	const t = str(raw.trigger_type);
	if ((TRIGGERS as string[]).includes(t)) return t as ScheduleTrigger;
	if (str(raw.every)) return "interval";
	if (str(raw.days)) return "weekly";
	if (str(raw.at)) return "daily";
	if (str(raw.schedule)) return "cron";
	return "once";
}

export function normalizeSchedule(v: unknown): ProjectSchedule {
	const raw = obj(v);
	return {
		id: str(raw.id),
		agent: str(raw.agent_name) || str(raw.agent_id),
		name: str(raw.name),
		project: str(raw.project),
		model: str(raw.model),
		prompt: str(raw.prompt),
		enabled: raw.enabled !== false,
		trigger: normalizeTrigger(raw),
		schedule: str(raw.schedule),
		every: str(raw.every),
		at: str(raw.at),
		days: str(raw.days),
		startAt: str(raw.start_at),
		timezone: str(raw.timezone),
		overlap: str(raw.overlap) === "skip" ? "skip" : "queue",
		missed: str(raw.missed) === "run_once" ? "run_once" : "skip",
		description: str(raw.description),
		nextRun: str(raw.next_run),
		history: arr(raw.history)
			.map((h) => {
				const run = obj(h);
				return {
					fireTime: str(run.fire_time),
					reason: str(run.reason),
					status: str(run.status),
					summary: str(run.summary),
					durationMS: num(run.duration_ms),
				};
			})
			.reverse(),
	};
}

/** The scheduled prompts of project from a task_list response. */
export function projectSchedules(
	v: unknown,
	project: string,
): ProjectSchedule[] {
	return arr(v)
		.filter((t) => str(obj(t).type) === "project")
		.map(normalizeSchedule)
		.filter((s) => s.project === project)
		.sort((a, b) => a.name.localeCompare(b.name));
}

export interface UpcomingRun {
	id: string;
	name: string;
	at: string;
}

export function normalizeUpcoming(v: unknown): UpcomingRun[] {
	return arr(v).map((u) => {
		const raw = obj(u);
		return { id: str(raw.task_id), name: str(raw.name), at: str(raw.at) };
	});
}

/**
 * What a waiting or paused job waits on, as the engine recorded it, with the
 * time left until a scheduled wakeup; empty when it records no reason.
 */
export function waitText(
	job: Pick<ProjectJob, "waitingReason" | "wakeAt">,
	now = Date.now(),
): string {
	if (!job.waitingReason) return "";
	const wake = Date.parse(job.wakeAt);
	return Number.isNaN(wake) || wake <= now
		? job.waitingReason
		: `${job.waitingReason} · wakes ${until(job.wakeAt, now)}`;
}

/** A compact "in 14m" / "in 3h" countdown to an RFC 3339 timestamp. */
export function until(iso: string, now = Date.now()): string {
	const t = Date.parse(iso);
	if (Number.isNaN(t)) return "";
	if (t <= now) return "now";
	return `in ${age(new Date(now - (t - now)).toISOString(), now)}`;
}

const FINISHED_RUNS = ["completed", "failed", "canceled", "skipped"];

/** The schedule's latest finished run, or its latest run when none finished. */
export function lastScheduleRun(s: ProjectSchedule): ScheduleRun | undefined {
	return (
		s.history.find((r) => FINISHED_RUNS.includes(r.status)) ?? s.history[0]
	);
}

/** The dot color of a schedule run's status. */
export function scheduleRunDot(status: string): string {
	switch (status) {
		case "completed":
			return "bg-green-500";
		case "failed":
			return "bg-red-500";
		case "running":
		case "queued":
		case "retrying":
			return "bg-blue-500";
		default:
			return "bg-gray-400";
	}
}

/** A schedule fire time as "Tue, Nov 21, 02:00" in the browser's zone. */
export function formatFire(iso: string): string {
	const t = Date.parse(iso);
	return Number.isNaN(t)
		? iso
		: new Date(t).toLocaleString(undefined, {
				weekday: "short",
				month: "short",
				day: "numeric",
				hour: "2-digit",
				minute: "2-digit",
			});
}

/** A schedule run's duration as "42s" or "5m 12s"; empty when unknown. */
export function runDuration(ms: number): string {
	if (!ms) return "";
	const s = Math.round(ms / 1000);
	return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${s % 60}s`;
}

/** The schedule's trigger in words, as the scheduler describes it. */
export function triggerSummary(s: ProjectSchedule): string {
	if (s.description) return s.description;
	const zone = ` (${s.timezone || "Local"})`;
	switch (s.trigger) {
		case "once":
			return s.startAt ? `once at ${formatFire(s.startAt)}` : "once";
		case "interval":
			return `every ${s.every}${zone}`;
		case "daily":
			return `daily at ${s.at}${zone}`;
		case "weekly":
			return `weekly on ${s.days} at ${s.at}${zone}`;
		case "cron":
			return `cron ${s.schedule}${zone}`;
	}
}

/** The prompt's first line, cut to max characters with an ellipsis. */
export function truncatePrompt(prompt: string, max = 80): string {
	const line = prompt.trim().split("\n")[0]?.trim() ?? "";
	return line.length > max ? `${line.slice(0, max - 1).trimEnd()}…` : line;
}

/** Whether a schedule matches the board's search text. */
export function scheduleMatches(s: ProjectSchedule, search: string): boolean {
	const q = search.trim().toLowerCase();
	if (!q) return true;
	return [s.id, s.name, s.prompt, triggerSummary(s)].some((f) =>
		f.toLowerCase().includes(q),
	);
}
