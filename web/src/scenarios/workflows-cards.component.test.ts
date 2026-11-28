import { expect, test } from "vitest";
import { commands, type Locator, page } from "vitest/browser";
import { currentURL, reload, renderApp } from "../component-test/app";
import {
	AGO,
	ARCHIVE,
	LOG_ALL,
	mockWorkflows,
	NOW,
	TRIAGE,
	TRIGGER_CONTEXT,
} from "./workflows-fixtures";
// The pages this file renders (see component-test/routes.ts).
import "../views/ChannelView.vue";
import "../views/SettingsView.vue";
import "../views/TaskView.vue";

// The channel chat's workflow cards and a workflow's detail view, from
// web/e2e/workflows.spec.ts.

const CHANNEL = "/agents/assistant/channels/Team%20Slack";

const count = (loc: Locator) => () => loc.elements().length;

test("the channel chat shows workflow cards, active first, with a remembered collapse and paused filter", async () => {
	mockWorkflows(
		[ARCHIVE, TRIAGE, LOG_ALL],
		[
			{
				id: "job-1",
				task_id: "assistant/triage",
				agent_id: "assistant",
				status: "in_progress",
				trigger: "message",
				prompt: "",
				attempts: 1,
				max_retries: 0,
				created_at: NOW,
				updated_at: NOW,
			},
			{
				id: "job-0",
				task_id: "assistant/log-all",
				agent_id: "assistant",
				status: "pending",
				trigger: "message",
				prompt: "",
				attempts: 0,
				max_retries: 0,
				created_at: NOW,
				updated_at: NOW,
			},
		],
	);
	await renderApp(CHANNEL);

	const cards = page.getByTestId("workflow-card");
	await expect.poll(count(cards)).toBe(3);
	await expect
		.element(cards.nth(0))
		.toHaveAttribute("data-workflow", "log-all");
	await expect.element(cards.nth(1)).toHaveAttribute("data-workflow", "triage");
	await expect
		.element(cards.nth(2))
		.toHaveAttribute("data-workflow", "archive");
	await expect.element(cards.nth(2)).toHaveAttribute("data-paused", "true");

	// Triggering and running cards carry the traveling border, in different colors.
	await expect
		.element(cards.nth(0))
		.toHaveAttribute("data-activity", "triggering");
	await expect
		.element(cards.nth(1))
		.toHaveAttribute("data-activity", "running");
	const colors = cards
		.elements()
		.slice(0, 2)
		.map((el) => (el as HTMLElement).style.getPropertyValue("--snake"));
	expect(colors[0]).not.toBe(colors[1]);
	const animation = getComputedStyle(
		cards.nth(1).element(),
		"::before",
	).animationName;
	expect(animation).toContain("workflow-snake");

	const hidePaused = page.getByTestId("workflow-strip-hide-paused");
	await expect.element(hidePaused).not.toBeChecked();
	await hidePaused.click();
	await expect.element(hidePaused).toBeChecked();
	await expect.poll(count(cards)).toBe(2);

	await page.getByTestId("workflow-strip-toggle").click();
	await expect
		.element(page.getByTestId("workflow-strip"))
		.toHaveAttribute("data-collapsed", "true");
	await reload();
	await expect
		.element(page.getByTestId("workflow-strip"))
		.toHaveAttribute("data-collapsed", "true");
	await expect.poll(count(cards)).toBe(2);

	// The message the workflow handled says so, and links to its run.
	const handled = page.getByTestId("chat-message-workflows");
	await expect.element(handled).toMatchTextContent("Handled by workflow");
	await expect
		.element(page.getByTestId("chat-message-workflow-link"))
		.toHaveAttribute("href", "/agents/assistant/tasks/triage/runs/job-1");
});

test("the traveling border does not travel under reduced motion", async () => {
	await commands.emulateMedia({ reducedMotion: "reduce" });
	mockWorkflows(
		[TRIAGE],
		[
			{
				id: "job-1",
				task_id: "assistant/triage",
				agent_id: "assistant",
				status: "in_progress",
				prompt: "",
				attempts: 1,
				max_retries: 0,
				created_at: NOW,
				updated_at: NOW,
			},
		],
	);
	await renderApp(CHANNEL);
	const card = page.getByTestId("workflow-card");
	await expect.element(card).toHaveAttribute("data-activity", "running");
	const animation = getComputedStyle(card.element(), "::before").animationName;
	expect(animation).toBe("none");
});

test("a workflow card opens its detail view with the run's trigger context", async () => {
	mockWorkflows(
		[TRIAGE],
		[
			{
				id: "job-1",
				task_id: "assistant/triage",
				agent_id: "assistant",
				status: "completed",
				trigger: "message",
				trigger_context: TRIGGER_CONTEXT,
				prompt: "",
				attempts: 1,
				max_retries: 0,
				created_at: AGO,
				updated_at: NOW,
			},
		],
	);
	await renderApp(CHANNEL);
	await page.getByTestId("workflow-card").click();
	await expect.poll(currentURL).toMatch(/\/agents\/assistant\/tasks\/triage$/);

	await expect
		.element(page.getByTestId("task-schedule"))
		.toMatchTextContent("Workflow");
	await expect.element(page.getByTestId("task-run-now")).toBeVisible();
	await expect
		.element(page.getByTestId("task-toggle-enabled"))
		.toHaveText(/Pause/);
	const ctx = page.getByTestId("trigger-context");
	await expect.element(ctx).toMatchTextContent("Team Slack · C1");
	await expect
		.element(page.getByTestId("trigger-context-before"))
		.toMatchTextContent("Bob: deploying now");
	await expect
		.element(page.getByTestId("trigger-context-message"))
		.toMatchTextContent("Alice: the app crashed");
	await expect
		.element(page.getByTestId("trigger-context-attachment"))
		.toMatchTextContent("crash.log");
	await expect
		.element(page.getByTestId("trigger-context-after"))
		.toMatchTextContent("Agent: Looking into it");

	// Settings opens the channel's Workflows tab on this workflow.
	await page.getByTestId("task-settings-link").click();
	await expect.element(page.getByTestId("workflow-form")).toBeVisible();
	await expect.element(page.getByTestId("workflow-name")).toHaveValue("triage");
});

test("workflows stay out of the Scheduled Tasks list", async () => {
	mockWorkflows([TRIAGE, { name: "hourly", every: "1h", prompt: "p" }]);
	await renderApp("/agents/assistant/tasks");
	await expect.poll(count(page.getByTestId("task-card"))).toBe(1);
});

test("workflow cards and the detail view show the compiled state and fallback runs", async () => {
	mockWorkflows(
		[
			{
				...TRIAGE,
				precompile: true,
				compiled: 'print("ok")',
				compiled_key: "k",
				script_failures: 1,
				script_last_error: "lookup failed",
			},
		],
		[
			{
				id: "job-1",
				task_id: "assistant/triage",
				agent_id: "assistant",
				status: "completed",
				trigger: "message",
				trigger_context: TRIGGER_CONTEXT,
				script_error: "lookup failed",
				prompt: "",
				attempts: 1,
				max_retries: 0,
				created_at: AGO,
				updated_at: NOW,
			},
		],
	);
	await renderApp(CHANNEL);
	const card = page.getByTestId("workflow-card");
	await expect
		.element(card.getByTestId("compiled-status-chip"))
		.toHaveText("Script failing");
	await expect
		.element(card.getByTestId("compiled-status-chip"))
		.toHaveAttribute("title", expect.stringMatching(/failed its last run/));
	await expect
		.element(card.getByTestId("workflow-card-last"))
		.toHaveAttribute("data-last", "fallback");
	await expect
		.element(card.getByTestId("workflow-card-last"))
		.toHaveText("last run fell back");

	await card.click();
	await expect.poll(currentURL).toMatch(/\/agents\/assistant\/tasks\/triage$/);
	await expect
		.element(
			page.getByTestId("settings-header").getByTestId("compiled-status-chip"),
		)
		.toHaveAttribute("data-state", "failing");
	await expect
		.element(page.getByTestId("task-script-failing"))
		.toMatchTextContent("last error: lookup failed");
	const stats = page.getByTestId("stat-row");
	await expect
		.element(stats)
		.toMatchTextContent("2compile attempts · last succeeded");
	await expect
		.element(stats.getByTitle("Succeeded via fallback: 1"))
		.toBeVisible();
});

test("workflow cards and the run history show a run that chose not to reply", async () => {
	mockWorkflows(
		[TRIAGE],
		[
			{
				id: "job-1",
				task_id: "assistant/triage",
				agent_id: "assistant",
				status: "completed",
				trigger: "message",
				trigger_context: TRIGGER_CONTEXT,
				delivery: {
					target: "reply",
					status: "skipped",
					message: "message was negative",
					at: NOW,
				},
				prompt: "",
				attempts: 1,
				max_retries: 0,
				created_at: AGO,
				updated_at: NOW,
			},
		],
	);
	await renderApp(CHANNEL);
	const card = page.getByTestId("workflow-card");
	const last = card.getByTestId("workflow-card-last");
	await expect.element(last).toHaveAttribute("data-last", "no_reply");
	await expect.element(last).toHaveText("last run: no reply");
	await expect
		.element(last)
		.toHaveAttribute("title", "Skipped reply: message was negative");

	await card.click();
	await expect.poll(currentURL).toMatch(/\/agents\/assistant\/tasks\/triage$/);
	await expect
		.element(page.getByTestId("task-run-skipped-reply"))
		.toHaveText("Skipped reply: message was negative");
});
