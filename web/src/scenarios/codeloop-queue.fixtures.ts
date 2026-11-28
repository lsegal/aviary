import { mockMCP } from "../component-test/server";

// Shared fixtures for codeloop-queue*.component.test.ts (#1636).
// Queueing pull requests for gh-safe-merge and Discussion threads for
// gh-discuss from a Code Loop board (#1095). Fixtures follow the payloads
// internal/projects' Manager returns.

const minutesAgo = (m: number) =>
	new Date(Date.now() - m * 60_000).toISOString();

export const TARGET = "lsegal/aviary";

const queuedActions = (status: string) => ({
	retry: ["paused", "complete", "failed"].includes(status),
	pause: ["queued", "active", "waiting"].includes(status),
	resume: status === "paused",
	stop: ["queued", "active", "waiting", "paused"].includes(status),
});

export const JOBS = [
	{
		key: `${TARGET}#218`,
		target: TARGET,
		number: 218,
		title: "Fix the scheduler's missed fires",
		status: "complete",
		state: "done",
		kind: "issue",
		agent: "claude",
		model: "opus",
		repository: TARGET,
		issue_url: `https://github.com/${TARGET}/issues/218`,
		pull_request: 230,
		pull_request_url: `https://github.com/${TARGET}/pull/230`,
		pull_request_state: "open",
		started: minutesAgo(40),
		actions: { retry: true, pause: false, resume: false, stop: false },
	},
	{
		key: `${TARGET}#1201`,
		target: TARGET,
		number: 1201,
		title: "Add a dark theme toggle",
		status: "active",
		state: "running",
		kind: "merge",
		agent: "claude",
		model: "opus",
		repository: TARGET,
		issue_url: `https://github.com/${TARGET}/pull/1201`,
		pull_request: 1201,
		started: minutesAgo(3),
		actions: queuedActions("active"),
	},
	// Safe merges run side by side (#1433), so two are merging at once.
	{
		key: `${TARGET}#1203`,
		target: TARGET,
		number: 1203,
		title: "Cache the board snapshot",
		status: "active",
		state: "running",
		kind: "merge",
		agent: "claude",
		model: "opus",
		repository: TARGET,
		issue_url: `https://github.com/${TARGET}/pull/1203`,
		pull_request: 1203,
		started: minutesAgo(2),
		actions: queuedActions("active"),
	},
	{
		key: `${TARGET}#1207`,
		target: TARGET,
		number: 1207,
		title: "Bump vite",
		status: "queued",
		state: "starting",
		kind: "merge",
		repository: TARGET,
		issue_url: `https://github.com/${TARGET}/pull/1207`,
		pull_request: 1207,
		queue_position: 1,
		started: minutesAgo(1),
		actions: queuedActions("queued"),
		retry_unavailable: "Already queued",
	},
	{
		key: `${TARGET}#1190`,
		target: TARGET,
		number: 1190,
		title: "Rewrite the poller",
		status: "failed",
		state: "failed",
		kind: "merge",
		repository: TARGET,
		issue_url: `https://github.com/${TARGET}/pull/1190`,
		pull_request: 1190,
		summary: "Skipped: the pull request conflicts with main",
		started: minutesAgo(20),
		actions: queuedActions("failed"),
	},
	{
		key: `${TARGET}#44`,
		target: TARGET,
		number: 44,
		title: "How do I watch a Project board?",
		status: "queued",
		state: "starting",
		kind: "discuss",
		repository: TARGET,
		issue_url: `https://github.com/${TARGET}/discussions/44`,
		started: minutesAgo(1),
		actions: queuedActions("queued"),
		retry_unavailable: "Already queued",
	},
];

const PULLS = [
	{
		number: 1210,
		title: "Fix typo in README",
		author: "octocat",
		url: `https://github.com/${TARGET}/pull/1210`,
		ci: "passing",
		review: "approved",
		mergeable: "mergeable",
	},
	{
		number: 1212,
		title: "Speed up the job list",
		author: "hubot",
		url: `https://github.com/${TARGET}/pull/1212`,
		ci: "pending",
		review: "review_required",
		mergeable: "conflicting",
	},
	{
		number: 1207,
		title: "Bump vite",
		author: "dependabot",
		url: `https://github.com/${TARGET}/pull/1207`,
		ci: "failing",
		review: "none",
		mergeable: "mergeable",
		owned_by: `${TARGET}#1207`,
	},
	{
		number: 230,
		title: "Fix the scheduler's missed fires",
		author: "lsegal",
		url: `https://github.com/${TARGET}/pull/230`,
		ci: "passing",
		review: "none",
		mergeable: "mergeable",
	},
];

// A scheduled task of the project, which shows as a scheduled card.
const SCHEDULE = {
	id: "coder/nightly-triage",
	name: "nightly-triage",
	agent_name: "coder",
	agent_id: "coder",
	type: "project",
	project: TARGET,
	prompt: "Triage issues opened since the last run and label them.",
	enabled: true,
	trigger_type: "daily",
	at: "02:00",
	description: "daily at 02:00",
	next_run: new Date(Date.now() + 42 * 60_000).toISOString(),
	created_at: "0001-01-01T00:00:00Z",
};

export function setup({
	noMerge = false,
	delivery = "poll",
	listing = "ok",
}: {
	noMerge?: boolean;
	delivery?: string;
	/** ok lists; unavailable returns the server's notice; error fails the call. */
	listing?: "ok" | "unavailable" | "error";
} = {}) {
	const calls: Record<string, Record<string, unknown>[]> = {};
	const record = (tool: string, args?: Record<string, unknown>) => {
		calls[tool] ??= [];
		calls[tool].push(args ?? {});
	};
	mockMCP({
		agent_list: [],
		config_get: {
			agents: [
				{
					name: "coder",
					model: "cli/claude/opus",
					projects: [{ target: TARGET }],
				},
			],
		},
		project_status: {
			identity: "7F3CA91B",
			running: true,
			targets: [TARGET],
			issue_counts: { [TARGET]: 3 },
			states: { starting: 2, paused: 0, running: 2, done: 1, failed: 1 },
			jobs: {
				running: 2,
				queued: 2,
				paused: 0,
				completed: 1,
				merged_24h: 0,
				failed: 1,
			},
			concurrency: 2,
			delivery,
			poll_interval: "30s",
			no_merge: noMerge,
			last_check: minutesAgo(0.1),
		},
		// The board reads its schedules' entries apart from its jobs, each
		// limited to the kinds and states it names, as the Manager does.
		project_job_list: (args?: Record<string, unknown>) => {
			record("project_job_list", args);
			const kinds = args?.kinds as string[] | undefined;
			const states = args?.states as string[] | undefined;
			const jobs = JOBS.filter(
				(j) =>
					(!kinds || kinds.includes(j.kind)) &&
					(!states || states.includes(j.state)),
			);
			return { jobs, total: jobs.length, offset: 0, limit: 500 };
		},
		task_list: [SCHEDULE],
		project_job_get: (args?: Record<string, unknown>) => ({
			...JOBS.find((j) => j.number === args?.number),
			scrollback: "",
			events: [],
		}),
		project_queue_candidates: (args?: Record<string, unknown>) => {
			record("project_queue_candidates", args);
			if (listing === "error") throw new Error("gh: connection refused");
			if (listing === "unavailable")
				return {
					target: TARGET,
					repository: TARGET,
					skill: args?.skill,
					no_merge: noMerge,
					unavailable:
						args?.skill === "gh-discuss"
							? "Can't list discussions: sign in to GitHub (gh auth login) to enable this. You can still queue by number."
							: "Can't list pull requests: sign in to GitHub (gh auth login) to enable this. You can still queue by number.",
				};
			if (args?.skill === "gh-discuss")
				return {
					target: TARGET,
					repository: TARGET,
					skill: "gh-discuss",
					no_merge: noMerge,
					discussions: [
						{
							number: 51,
							title: "Is Windows supported?",
							author: "ada",
							url: "",
							category: "Q&A",
							comments: 0,
							answered: false,
						},
						{
							number: 44,
							title: "How do I watch a Project board?",
							author: "bob",
							url: "",
							category: "Q&A",
							comments: 0,
							answered: false,
							owned_by: `${TARGET}#44`,
						},
					],
				};
			return {
				target: TARGET,
				repository: TARGET,
				skill: "gh-safe-merge",
				no_merge: noMerge,
				pull_requests: PULLS,
			};
		},
		project_job_queue: (args?: Record<string, unknown>) => {
			record("project_job_queue", args);
			const numbers = (args?.numbers as number[]) ?? [];
			return {
				target: TARGET,
				skill: args?.skill,
				results: numbers.map((number, i) => ({
					number,
					queued: true,
					key: `${TARGET}#${number}`,
					...(args?.skill === "gh-safe-merge" ? { position: i + 3 } : {}),
				})),
			};
		},
	});
	return calls;
}

export const BOARD = `/agents/coder/projects/${encodeURIComponent(TARGET)}`;
