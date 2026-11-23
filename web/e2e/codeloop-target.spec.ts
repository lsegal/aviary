import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";
import { chooseOption, listbox, searchOption } from "./helpers/select";

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

async function mockCodeLoop(
	page: Page,
	tasks: Task[],
	jobs: Record<string, unknown>[] = [],
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
	await setAuthToken(page);
	await mockMCP(page, {
		agent_list: [
			{ id: "assistant", name: "assistant", model: "x/y", state: "idle" },
		],
		config_get: () => config,
		config_save: (args) => {
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
			return { jobs: JOBS, total: JOBS.length };
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

test("the workflow form sends its output to a Code Loop job picked from a grouped, searchable list", async ({
	page,
}) => {
	const { calls } = await mockCodeLoop(page, []);
	await page.goto(
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
	await expect(list.getByText("o/r", { exact: true })).toBeVisible();
	await expect(list.getByText("o/web", { exact: true })).toBeVisible();
	await expect(list.getByRole("option")).toHaveCount(3);
	await expect(
		list.getByRole("option", { name: /^#15 Flaky upload/ }),
	).toContainText("paused");
	await page.keyboard.press("Escape");

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

test("the workflow form warns when its Code Loop job no longer exists", async ({
	page,
}) => {
	await mockCodeLoop(page, [
		{
			name: "relay",
			trigger: "message",
			channel: "Team Slack",
			prompt: "x",
			target: "codeloop:o/r#99",
		},
	]);
	await page.goto(
		"/agents/assistant/channels/Team%20Slack/settings?tab=workflows",
	);
	await expect(page.getByTestId("workflow-row")).toContainText(
		"sends to Code Loop o/r#99",
	);
	await page.getByTestId("workflow-edit").click();
	await expect(
		page.getByTestId("workflow-form").getByTestId("codeloop-job-warning"),
	).toContainText("o/r#99 no longer exists");
});

test("the workflow form shows its Code Loop job's label once the jobs load", async ({
	page,
}) => {
	// The jobs arrive after the picker renders its target.
	await mockCodeLoop(
		page,
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
		500,
	);
	await page.goto(
		"/agents/assistant/channels/Team%20Slack/settings?tab=workflows",
	);
	await page.getByTestId("workflow-edit").click();
	const combobox = page
		.getByTestId("workflow-form")
		.getByRole("combobox", { name: "Code Loop job" });
	await expect(combobox).toHaveAttribute("data-value", "codeloop:o/r#15");
	await expect(combobox).toHaveValue("#15 Flaky upload");
});

test("the task form sends its output to a Code Loop job", async ({ page }) => {
	const { calls } = await mockCodeLoop(page, [
		{ name: "digest", schedule: "0 9 * * *", prompt: "report" },
	]);
	await page.goto("/agents/assistant/tasks?task=digest");

	await chooseOption(page.getByTestId("task-send-via"), "Code Loop job");
	await expect(page.getByTestId("codeloop-job-warning")).toHaveCount(0);
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

test("the task view links its Code Loop target and the run log records the delivery", async ({
	page,
}) => {
	await mockCodeLoop(
		page,
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
	await page.goto("/agents/assistant/tasks/digest");
	const link = page.getByTestId("codeloop-target-link");
	await expect(link).toHaveText("Code Loop o/r#12");
	await expect(link).toHaveAttribute(
		"href",
		"/agents/assistant/projects/o%2Fr/jobs/12",
	);

	const delivery = page.getByTestId("task-run-log-delivery");
	await expect(delivery).toHaveAttribute("data-status", "delivered");
	await expect(delivery).toContainText("delivered to Code Loop o/r#12");
	await expect(page.getByTestId("task-run-log-delivery-link")).toHaveAttribute(
		"href",
		"/agents/assistant/projects/o%2Fr/jobs/12",
	);
});

test("the task view warns when its Code Loop job no longer exists", async ({
	page,
}) => {
	await mockCodeLoop(page, [
		{
			name: "digest",
			schedule: "0 9 * * *",
			prompt: "report",
			target: "codeloop:o/r#99",
		},
	]);
	await page.goto("/agents/assistant/tasks/digest");
	await expect(page.getByTestId("codeloop-target-warning")).toContainText(
		"job no longer exists",
	);
});

// The resume and reopen delivery options (#1008) are checkboxes beside the
// job picker and are stored on the target.

test("the workflow form saves the resume and reopen options on its Code Loop target", async ({
	page,
}) => {
	const { calls } = await mockCodeLoop(page, []);
	await page.goto(
		"/agents/assistant/channels/Team%20Slack/settings?tab=workflows",
	);
	await page.getByTestId("workflow-add").click();
	await page.getByTestId("workflow-name").fill("relay");
	await page.getByTestId("workflow-content").fill("Summarize it.");
	await chooseOption(page.getByTestId("workflow-send-via"), "a Code Loop job");

	// The options appear once a job is picked and default to off.
	await expect(page.getByTestId("codeloop-resume")).toHaveCount(0);
	await searchOption(
		page.getByRole("combobox", { name: "Code Loop job" }),
		"upload",
		/^#15 Flaky upload/,
	);
	await expect(page.getByTestId("codeloop-resume")).not.toBeChecked();
	await expect(page.getByTestId("codeloop-reopen")).not.toBeChecked();
	await page.getByTestId("codeloop-resume").check();
	await page.getByTestId("codeloop-reopen").check();
	await page.getByTestId("workflow-save").click();

	await expect.poll(() => calls.task_schedule?.length ?? 0).toBe(1);
	expect(calls.task_schedule[0]).toMatchObject({
		name: "relay",
		target: "codeloop:o/r#15?resume,reopen",
	});
});

test("the task form keeps its Code Loop options when the job changes and clears them when unchecked", async ({
	page,
}) => {
	const { calls } = await mockCodeLoop(page, [
		{
			name: "digest",
			schedule: "0 9 * * *",
			prompt: "report",
			target: "codeloop:o/r#12?resume,reopen",
		},
	]);
	await page.goto("/agents/assistant/tasks?task=digest");

	const combobox = page.getByRole("combobox", { name: "Code Loop job" });
	// The picker shows the bare job; its options are the checkboxes.
	await expect(combobox).toHaveAttribute("data-value", "codeloop:o/r#12");
	// It shows the job's label, not the raw target, once the jobs load (#1011).
	await expect(combobox).toHaveValue("#12 Fix login");
	await expect(page.getByTestId("codeloop-job-warning")).toHaveCount(0);
	await expect(page.getByTestId("codeloop-resume")).toBeChecked();
	await expect(page.getByTestId("codeloop-reopen")).toBeChecked();

	await searchOption(combobox, "dark", /^#3 Dark mode/);
	await expect(page.getByTestId("codeloop-reopen")).toBeChecked();
	await page.getByTestId("codeloop-resume").uncheck();
	await page.getByRole("button", { name: "Save Changes" }).click();

	await expect.poll(() => calls.config_save?.length ?? 0).toBe(1);
	const saved = JSON.parse(String(calls.config_save[0].config));
	expect(saved.agents[0].tasks[0].target).toBe("codeloop:o/web#3?reopen");
});
