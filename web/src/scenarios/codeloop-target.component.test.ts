import { expect, test } from "vitest";
import { commands, page } from "vitest/browser";
import { goto, renderApp } from "../component-test/app";
import { declarePages } from "../component-test/routes";
import { mockMCP } from "../component-test/server";
import { chooseOption, listbox, searchOption } from "./helpers/select";

// The pages this file renders (see component-test/routes.ts).
declarePages(
	() => import("../views/SettingsView.vue"),
	() => import("../views/TaskView.vue"),
);

// Code Loop jobs as a task's or workflow's output target (#999): the shared
// job picker in both forms, the target link in the task view, and the run
// log's delivery line.

const NOW = new Date().toISOString();

interface Task {
	name: string;
	prompt?: string;
	schedule?: string;
	trigger?: string;
	channel?: string;
	target?: string;
}

const JOBS = [
	{
		key: "o/r#12",
		target: "o/r",
		number: 12,
		title: "Fix login",
		status: "active",
		state: "running",
		kind: "issue",
	},
	{
		key: "o/r#15",
		target: "o/r",
		number: 15,
		title: "Flaky upload",
		status: "paused",
		state: "paused",
		kind: "issue",
	},
	{
		key: "o/web#3",
		target: "o/web",
		number: 3,
		title: "Dark mode",
		status: "waiting",
		state: "paused",
		kind: "issue",
	},
];

// A Discussion job and a schedule's one entry, whose latest run is numbered by
// its fire time and whose recent runs include an earlier one (#1007).
const SCHEDULE_RUN = 1763888400;
const EARLIER_RUN = 1763802000;
const OTHER_KINDS = [
	{
		key: "o/r#8",
		target: "o/r",
		number: 8,
		title: "How do I deploy?",
		status: "active",
		state: "running",
		kind: "discussion",
	},
	{
		key: `o/r#${SCHEDULE_RUN}`,
		target: "o/r",
		number: SCHEDULE_RUN,
		title: "Nightly digest",
		status: "complete",
		state: "scheduled",
		kind: "schedule",
		schedule_id: "assistant/nightly",
		recent_runs: [
			{ number: SCHEDULE_RUN, status: "complete" },
			{ number: EARLIER_RUN, status: "complete" },
		],
	},
];

function mockCodeLoop(
	tasks: Task[],
	jobs: Record<string, unknown>[] = [],
	codeLoopJobs: Record<string, unknown>[] = JOBS,
	jobsDelayMs = 0,
) {
	const config = {
		agents: [
			{
				name: "assistant",
				model: "x/y",
				channels: [
					{
						type: "slack",
						id: "team-slack",
						name: "Team Slack",
						deliver_to: "C1",
					},
				],
				projects: [{ target: "o/r" }, { target: "o/web" }],
				tasks: tasks.map((t) => ({ ...t })),
			},
		],
	};
	const calls: Record<string, Record<string, unknown>[]> = {};
	const record = (tool: string, args?: Record<string, unknown>) => {
		calls[tool] ??= [];
		calls[tool].push(args ?? {});
	};
	mockMCP({
		agent_list: [
			{ id: "assistant", name: "assistant", model: "x/y", state: "idle" },
		],
		config_get: () => config,
		config_save: (args?: Record<string, unknown>) => {
			record("config_save", { ...args });
			return "ok";
		},
		config_validate: [],
		auth_list: [],
		tool_list: [],
		session_list: [
			{ id: "main", agent_id: "assistant", name: "main", created_at: NOW },
		],
		channel_sessions: [],
		session_messages: [],
		workflow_entries: [],
		workflow_preview: [],
		project_job_list: async (args?: Record<string, unknown>) => {
			record("project_job_list", args);
			await new Promise((r) => setTimeout(r, jobsDelayMs));
			return { jobs: codeLoopJobs, total: codeLoopJobs.length };
		},
		job_query: jobs,
		job_list: (args?: Record<string, unknown>) =>
			jobs.filter((j) => j.task_id === args?.task),
		job_logs: "status: working",
		task_session: { id: "task-session", agent_id: "assistant" },
		task_list: () =>
			config.agents[0].tasks.map((t) => ({
				id: `assistant/${t.name}`,
				agent_name: "assistant",
				name: t.name,
				enabled: true,
				trigger_type: t.trigger === "message" ? "message" : "cron",
				description:
					t.trigger === "message" ? `messages on ${t.channel}` : "every day",
				history: [],
			})),
		task_schedule: (args?: Record<string, unknown>) => {
			record("task_schedule", args);
			return "created";
		},
		task_update: (args?: Record<string, unknown>) => {
			record("task_update", args);
			return "updated";
		},
	});
	return { calls };
}

test("the workflow form sends its output to a Code Loop job picked from a grouped, searchable list", async () => {
	const { calls } = mockCodeLoop([]);
	await renderApp(
		"/agents/assistant/channels/Team%20Slack/settings?tab=workflows",
	);
	await page.getByTestId("workflow-add").click();
	await page.getByTestId("workflow-name").fill("relay");
	await page.getByTestId("workflow-content").fill("Summarize it.");

	await chooseOption(page.getByTestId("workflow-send-via"), "a Code Loop job");
	const picker = page.getByTestId("codeloop-job-select");
	await picker.click();
	const list = listbox(picker);
	// Jobs are grouped by project and show their number, title and status.
	await expect.element(list.getByText("o/r", { exact: true })).toBeVisible();
	await expect.element(list.getByText("o/web", { exact: true })).toBeVisible();
	await expect.poll(() => list.getByRole("option").elements().length).toBe(3);
	await expect
		.element(list.getByRole("option", { name: /^#15 Flaky upload/ }))
		.toMatchTextContent("paused");
	await commands.keyPress("Escape");

	await searchOption(
		page.getByRole("combobox", { name: "Code Loop job" }),
		"dark",
		/^#3 Dark mode/,
	);
	await page.getByTestId("workflow-save").click();

	await expect.poll(() => calls.task_schedule?.length ?? 0).toBe(1);
	expect(calls.task_schedule[0]).toMatchObject({
		name: "relay",
		target: "codeloop:o/web#3",
	});
});

test("the workflow form warns when its Code Loop job no longer exists", async () => {
	mockCodeLoop([
		{
			name: "relay",
			trigger: "message",
			channel: "Team Slack",
			prompt: "x",
			target: "codeloop:o/r#99",
		},
	]);
	await renderApp(
		"/agents/assistant/channels/Team%20Slack/settings?tab=workflows",
	);
	await expect
		.element(page.getByTestId("workflow-row"))
		.toMatchTextContent("sends to Code Loop o/r#99");
	await page.getByTestId("workflow-edit").click();
	await expect
		.element(
			page.getByTestId("workflow-form").getByTestId("codeloop-job-warning"),
		)
		.toMatchTextContent("o/r#99 no longer exists");
});

test("the workflow form shows its Code Loop job's label once the jobs load", async () => {
	// The jobs arrive after the picker renders its target.
	mockCodeLoop(
		[
			{
				name: "relay",
				trigger: "message",
				channel: "Team Slack",
				prompt: "x",
				target: "codeloop:o/r#15",
			},
		],
		[],
		JOBS,
		500,
	);
	await renderApp(
		"/agents/assistant/channels/Team%20Slack/settings?tab=workflows",
	);
	await page.getByTestId("workflow-edit").click();
	const combobox = page
		.getByTestId("workflow-form")
		.getByRole("combobox", { name: "Code Loop job" });
	await expect
		.element(combobox)
		.toHaveAttribute("data-value", "codeloop:o/r#15");
	await expect.element(combobox).toHaveValue("#15 Flaky upload");
});

test("the task form sends its output to a Code Loop job", async () => {
	const { calls } = mockCodeLoop([
		{ name: "digest", schedule: "0 9 * * *", prompt: "report" },
	]);
	await renderApp("/agents/assistant/tasks?task=digest");

	await chooseOption(page.getByTestId("task-send-via"), "Code Loop job");
	await expect
		.poll(() => page.getByTestId("codeloop-job-warning").elements().length)
		.toBe(0);
	await searchOption(
		page.getByRole("combobox", { name: "Code Loop job" }),
		"login",
		/^#12 Fix login/,
	);
	await page.getByRole("button", { name: "Save Changes" }).click();

	await expect.poll(() => calls.config_save?.length ?? 0).toBe(1);
	const saved = JSON.parse(String(calls.config_save[0].config));
	expect(saved.agents[0].tasks[0].target).toBe("codeloop:o/r#12");
});

test("the task view links its Code Loop target and the run log records the delivery", async () => {
	mockCodeLoop(
		[
			{
				name: "digest",
				schedule: "0 9 * * *",
				prompt: "report",
				target: "codeloop:o/r#12",
			},
		],
		[
			{
				id: "job-1",
				task_id: "assistant/digest",
				agent_id: "assistant",
				prompt: "report",
				status: "completed",
				attempts: 1,
				max_retries: 1,
				created_at: NOW,
				updated_at: NOW,
				delivery: {
					target: "codeloop:o/r#12",
					status: "delivered",
					message: "delivered to Code Loop o/r#12",
					project: "o/r",
					number: 12,
					owner: "assistant",
					at: NOW,
				},
			},
		],
	);
	await renderApp("/agents/assistant/tasks/digest");
	const link = page.getByTestId("codeloop-target-link");
	await expect.element(link).toHaveText("Code Loop o/r#12");
	await expect
		.element(link)
		.toHaveAttribute("href", "/agents/assistant/projects/o%2Fr/jobs/12");

	const delivery = page.getByTestId("task-run-log-delivery");
	await expect.element(delivery).toHaveAttribute("data-status", "delivered");
	await expect
		.element(delivery)
		.toMatchTextContent("delivered to Code Loop o/r#12");
	await expect
		.element(page.getByTestId("task-run-log-delivery-link"))
		.toHaveAttribute("href", "/agents/assistant/projects/o%2Fr/jobs/12");
});

test("the task view warns when its Code Loop job no longer exists", async () => {
	mockCodeLoop([
		{
			name: "digest",
			schedule: "0 9 * * *",
			prompt: "report",
			target: "codeloop:o/r#99",
		},
	]);
	await renderApp("/agents/assistant/tasks/digest");
	await expect
		.element(page.getByTestId("codeloop-target-warning"))
		.toMatchTextContent("job no longer exists");
});

test("the job picker lists Discussion jobs and schedules alongside issue jobs", async () => {
	const { calls } = mockCodeLoop(
		[{ name: "digest", schedule: "0 9 * * *", prompt: "report" }],
		[],
		[...JOBS, ...OTHER_KINDS],
	);
	await renderApp("/agents/assistant/tasks?task=digest");

	await chooseOption(page.getByTestId("task-send-via"), "Code Loop job");
	const picker = page.getByTestId("codeloop-job-select");
	await picker.click();
	const list = listbox(picker);
	await expect.poll(() => list.getByRole("option").elements().length).toBe(5);
	await expect
		.element(list.getByRole("option", { name: /^#8 How do I deploy\?/ }))
		.toMatchTextContent("discussion · active");
	// A schedule is named by its title; its number is a fire time.
	await expect
		.element(list.getByRole("option", { name: /^Nightly digest/ }))
		.toMatchTextContent("schedule · complete");
	await commands.keyPress("Escape");
	expect(calls.project_job_list.at(-1)?.kinds).toEqual([
		"issue",
		"discussion",
		"schedule",
	]);

	await searchOption(
		page.getByRole("combobox", { name: "Code Loop job" }),
		"nightly",
		/^Nightly digest/,
	);
	await page.getByRole("button", { name: "Save Changes" }).click();
	await expect.poll(() => calls.config_save?.length ?? 0).toBe(1);
	const saved = JSON.parse(String(calls.config_save[0].config));
	expect(saved.agents[0].tasks[0].target).toBe(`codeloop:o/r#${SCHEDULE_RUN}`);
});

test("a target saved from an earlier run of a schedule still names the schedule", async () => {
	mockCodeLoop(
		[
			{
				name: "digest",
				schedule: "0 9 * * *",
				prompt: "report",
				target: `codeloop:o/r#${EARLIER_RUN}`,
			},
		],
		[],
		[...JOBS, ...OTHER_KINDS],
	);
	await renderApp("/agents/assistant/tasks/digest");
	await expect.element(page.getByTestId("codeloop-target-link")).toBeVisible();
	await expect
		.poll(() => page.getByTestId("codeloop-target-warning").elements().length)
		.toBe(0);

	await goto("/agents/assistant/tasks?task=digest");
	const picker = page.getByTestId("codeloop-job-select");
	await picker.click();
	// The earlier run is offered as the schedule, not as a missing job.
	const list = listbox(picker);
	await expect
		.poll(
			() =>
				list.getByRole("option", { name: /^Nightly digest/ }).elements().length,
		)
		.toBe(2);
	await expect
		.poll(
			() =>
				list.getByRole("option", { name: /no longer exists/ }).elements()
					.length,
		)
		.toBe(0);
	await expect
		.poll(() => page.getByTestId("codeloop-job-warning").elements().length)
		.toBe(0);
	// Resume and reopen only apply to issue jobs.
	await expect
		.poll(() => page.getByTestId("codeloop-resume").elements().length)
		.toBe(0);
});

// The resume and reopen delivery options (#1008) are checkboxes beside the
// job picker and are stored on the target.

test("the workflow form saves the resume and reopen options on its Code Loop target", async () => {
	const { calls } = mockCodeLoop([]);
	await renderApp(
		"/agents/assistant/channels/Team%20Slack/settings?tab=workflows",
	);
	await page.getByTestId("workflow-add").click();
	await page.getByTestId("workflow-name").fill("relay");
	await page.getByTestId("workflow-content").fill("Summarize it.");
	await chooseOption(page.getByTestId("workflow-send-via"), "a Code Loop job");

	// The options appear once a job is picked and default to off.
	await expect
		.poll(() => page.getByTestId("codeloop-resume").elements().length)
		.toBe(0);
	await searchOption(
		page.getByRole("combobox", { name: "Code Loop job" }),
		"upload",
		/^#15 Flaky upload/,
	);
	await expect.element(page.getByTestId("codeloop-resume")).not.toBeChecked();
	await expect.element(page.getByTestId("codeloop-reopen")).not.toBeChecked();
	await page.getByTestId("codeloop-resume").click();
	await page.getByTestId("codeloop-reopen").click();
	await page.getByTestId("workflow-save").click();

	await expect.poll(() => calls.task_schedule?.length ?? 0).toBe(1);
	expect(calls.task_schedule[0]).toMatchObject({
		name: "relay",
		target: "codeloop:o/r#15?resume,reopen",
	});
});

test("the task form keeps its Code Loop options when the job changes and clears them when unchecked", async () => {
	const { calls } = mockCodeLoop([
		{
			name: "digest",
			schedule: "0 9 * * *",
			prompt: "report",
			target: "codeloop:o/r#12?resume,reopen",
		},
	]);
	await renderApp("/agents/assistant/tasks?task=digest");

	const combobox = page.getByRole("combobox", { name: "Code Loop job" });
	// The picker shows the bare job; its options are the checkboxes.
	await expect
		.element(combobox)
		.toHaveAttribute("data-value", "codeloop:o/r#12");
	// It shows the job's label, not the raw target, once the jobs load (#1011).
	await expect.element(combobox).toHaveValue("#12 Fix login");
	await expect
		.poll(() => page.getByTestId("codeloop-job-warning").elements().length)
		.toBe(0);
	await expect.element(page.getByTestId("codeloop-resume")).toBeChecked();
	await expect.element(page.getByTestId("codeloop-reopen")).toBeChecked();

	await searchOption(combobox, "dark", /^#3 Dark mode/);
	await expect.element(page.getByTestId("codeloop-reopen")).toBeChecked();
	await page.getByTestId("codeloop-resume").click();
	await page.getByRole("button", { name: "Save Changes" }).click();

	await expect.poll(() => calls.config_save?.length ?? 0).toBe(1);
	const saved = JSON.parse(String(calls.config_save[0].config));
	expect(saved.agents[0].tasks[0].target).toBe("codeloop:o/web#3?reopen");
});
