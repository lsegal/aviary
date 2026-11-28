import { afterEach, expect, test, vi } from "vitest";
import { commands, type Locator, page } from "vitest/browser";
import { currentURL, renderApp } from "../component-test/app";
import { chooseOption } from "./helpers/select";
import {
	AGO,
	LOG_ALL,
	mockWorkflows,
	NOW,
	nativeDialogs,
	TRIAGE,
} from "./workflows-fixtures";
// The pages this file renders (see component-test/routes.ts).
import "../views/SettingsView.vue";

// The channel settings' Workflows tab, from web/e2e/workflows.spec.ts. The
// cached-analysis scenario stays in that spec: it needs real page reloads
// and a second browser, since the editor's analyses are kept in a
// module-level cache that only a new page drops.

afterEach(() => {
	vi.restoreAllMocks();
});

const WORKFLOWS_TAB =
	"/agents/assistant/channels/Team%20Slack/settings?tab=workflows";

const count = (loc: Locator) => () => loc.elements().length;

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

/** Checks or unchecks a checkbox, as Playwright's check and uncheck. */
async function setChecked(loc: Locator, checked: boolean) {
	if (checked) await expect.element(loc).not.toBeChecked();
	else await expect.element(loc).toBeChecked();
	await loc.click();
	if (checked) await expect.element(loc).toBeChecked();
	else await expect.element(loc).not.toBeChecked();
}

function editButton(name: string) {
	return page
		.locator(`[data-testid=workflow-row][data-workflow=${name}]`)
		.getByTestId("workflow-edit");
}

test("the Workflows tab adds a workflow with an entry, a match and a preview", async () => {
	const { calls } = mockWorkflows([TRIAGE]);
	await renderApp("/agents/assistant/channels/Team%20Slack/settings");

	await page.getByTestId("channel-tab-workflows").click();
	await expect.poll(currentURL).toMatch(/tab=workflows/);
	await expect.poll(count(page.getByTestId("workflow-row"))).toBe(1);
	await expect
		.element(page.getByTestId("workflow-row"))
		.toMatchTextContent("messages matching *crash* on C1");

	await page.getByTestId("workflow-add").click();
	// An empty match takes over every message; the form says so.
	await expect
		.element(page.getByTestId("workflow-takeover-hint"))
		.toMatchTextContent("the agent won't reply on its own");
	await expect
		.poll(count(page.getByTestId("workflow-preview-message")))
		.toBe(2);
	await expect
		.element(page.getByTestId("workflow-preview-count"))
		.toHaveText("2 of 2 recent messages");

	await page.getByTestId("workflow-name").fill("crashes");
	await page.getByTestId("workflow-match").fill("*crash*");
	await expect.poll(count(page.getByTestId("workflow-takeover-hint"))).toBe(0);
	await expect
		.element(page.getByTestId("workflow-preview-count"))
		.toHaveText("1 of 2 recent messages");
	await expect
		.element(
			page.locator("[data-testid=workflow-preview-message][data-matches=true]"),
		)
		.toMatchTextContent("the app crashed");

	await chooseOption(page.getByTestId("workflow-entry"), "#general (C1)");
	await page.getByTestId("workflow-content").fill("Reply with next steps.");
	await page.getByTestId("workflow-save").click();

	await expect.poll(count(page.getByTestId("workflow-row"))).toBe(2);
	expect(calls.task_schedule?.[0]).toMatchObject({
		agent: "assistant",
		name: "crashes",
		trigger: "message",
		channel: "Team Slack",
		entry: "C1",
		match: "*crash*",
		target: "reply",
		content: "Reply with next steps.",
	});
});

test("the Workflows tab pauses and deletes a workflow", async () => {
	const { calls } = mockWorkflows([TRIAGE]);
	await renderApp(WORKFLOWS_TAB);
	await page.getByTestId("task-enabled").click();
	await expect
		.poll(() => calls.task_update?.[0])
		.toMatchObject({
			name: "assistant/triage",
			enabled: false,
		});
	await expect
		.element(page.getByTestId("workflow-row"))
		.toMatchTextContent("Paused");

	const dialogs = nativeDialogs();
	await page.getByTestId("workflow-delete").click();
	const dialog = page.getByRole("alertdialog", {
		name: "Delete workflow triage?",
	});
	await expect
		.element(dialog)
		.toMatchTextContent("deletes the workflow's definition");
	await expect
		.element(dialog)
		.toMatchTextContent("Delete session data and files");
	await expect
		.element(page.getByTestId("workflow-delete-data-checkbox"))
		.toBeChecked();
	await expect.element(dialog).toMatchTextContent("can't be undone");
	await expect.poll(count(page.getByTestId("workflow-delete-runs"))).toBe(0);

	// Cancel and Esc both keep the workflow.
	await dialog.getByRole("button", { name: "Cancel" }).click();
	await expect.poll(count(dialog)).toBe(0);
	await page.getByTestId("workflow-delete").click();
	await expect.element(dialog).toBeVisible();
	await commands.keyPress("Escape");
	await expect.poll(count(dialog)).toBe(0);
	expect(calls.task_delete).toBeUndefined();
	await expect.poll(count(page.getByTestId("workflow-row"))).toBe(1);

	// Unchecked keeps its session, as a regular chat, and its run history.
	await page.getByTestId("workflow-delete").click();
	await setChecked(page.getByTestId("workflow-delete-data-checkbox"), false);
	await expect
		.element(page.getByTestId("workflow-delete-data-summary"))
		.toMatchTextContent("Keeps its data on disk");
	await dialog.getByRole("button", { name: "Delete", exact: true }).click();
	await expect.poll(count(dialog)).toBe(0);
	await expect.element(page.getByTestId("workflows-empty")).toBeVisible();
	await expect
		.element(page.getByTestId("toast"))
		.toHaveText("Deleted workflow triage");
	expect(calls.task_delete?.[0]).toEqual({
		name: "assistant/triage",
		delete_data: false,
	});
	expect(calls.task_stop?.[0]).toMatchObject({ name: "assistant/triage" });
	expect(dialogs).toEqual([]);
});

test("the workflow edit form's Danger Zone deletes the workflow and returns to the list (#1311)", async () => {
	const { calls } = mockWorkflows([TRIAGE, LOG_ALL]);
	const dialogs = nativeDialogs();
	await renderApp(WORKFLOWS_TAB);

	// A new workflow has nothing to delete.
	await page.getByTestId("workflow-add").click();
	await expect.element(page.getByTestId("workflow-form")).toBeVisible();
	await expect.poll(count(page.getByTestId("workflow-danger-zone"))).toBe(0);
	await page.getByRole("button", { name: "Cancel" }).click();

	await editButton("triage").click();
	const zone = page.getByTestId("workflow-danger-zone");
	await expect.element(zone).toMatchTextContent("Danger Zone");
	await expect
		.element(zone)
		.toMatchTextContent("definition and its run history");

	const dialog = page.getByRole("alertdialog", {
		name: "Delete workflow triage?",
	});
	await zone.getByRole("button", { name: "Delete workflow" }).click();
	await expect.element(dialog).toBeVisible();
	await commands.keyPress("Escape");
	await expect.poll(count(dialog)).toBe(0);
	await expect.element(page.getByTestId("workflow-form")).toBeVisible();

	await zone.getByRole("button", { name: "Delete workflow" }).click();
	await dialog.getByRole("button", { name: "Delete", exact: true }).click();
	await expect.poll(count(page.getByTestId("workflow-form"))).toBe(0);
	await expect.poll(count(page.getByTestId("workflow-row"))).toBe(1);
	await expect
		.element(page.getByTestId("workflow-row"))
		.toHaveAttribute("data-workflow", "log-all");
	await expect
		.element(page.getByTestId("toast"))
		.toHaveText("Deleted workflow triage");
	expect(calls.task_delete).toEqual([
		{ name: "assistant/triage", delete_data: true },
	]);
	expect(dialogs).toEqual([]);
});

test("deleting a running workflow says its runs stop, and stops them (#1311)", async () => {
	const { calls } = mockWorkflows(
		[TRIAGE],
		[
			{
				id: "job-1",
				task_id: "assistant/triage",
				agent_id: "assistant",
				status: "in_progress",
				created_at: NOW,
				updated_at: NOW,
			},
			{
				id: "job-0",
				task_id: "assistant/triage",
				agent_id: "assistant",
				status: "completed",
				created_at: AGO,
				updated_at: AGO,
			},
		],
	);
	const dialogs = nativeDialogs();
	await renderApp(WORKFLOWS_TAB);
	await page.getByTestId("workflow-delete").click();
	await expect
		.element(page.getByTestId("workflow-delete-runs"))
		.toHaveText(
			"A run is in progress or queued. Deleting stops it, and the workflow won't run again.",
		);
	await page
		.getByTestId("workflow-delete-dialog")
		.getByRole("button", { name: "Delete", exact: true })
		.click();
	await expect.element(page.getByTestId("workflows-empty")).toBeVisible();
	expect(calls.task_stop).toEqual([{ name: "assistant/triage" }]);
	expect(dialogs).toEqual([]);
});

test("the workflow delete modal reports a failure, traps focus and fits a phone (#1311)", async () => {
	await page.viewport(375, 667);
	const { calls, failures } = mockWorkflows([TRIAGE]);
	failures.task_delete = "permission denied";
	const dialogs = nativeDialogs();
	await renderApp(WORKFLOWS_TAB);
	await page.getByTestId("workflow-delete").click();
	const dialog = page.getByTestId("workflow-delete-dialog");
	await expect.element(dialog).toHaveAccessibleName("Delete workflow triage?");
	await expect.element(dialog).toHaveAccessibleDescription(/can't be undone/);
	const box = dialog.element().getBoundingClientRect();
	expect(box.x).toBeGreaterThanOrEqual(0);
	expect(box.x + box.width).toBeLessThanOrEqual(375);

	// Tabbing cycles inside the modal.
	for (let i = 0; i < 4; i++) {
		await commands.keyPress("Tab");
		expect(dialog.element().contains(document.activeElement)).toBe(true);
	}

	await dialog.getByRole("button", { name: "Delete", exact: true }).click();
	await expect
		.element(page.getByTestId("workflow-delete-dialog-error"))
		.toHaveText(/permission denied/);
	await expect.element(dialog).toBeVisible();
	expect(calls.task_delete).toHaveLength(1);
	expect(calls.task_stop).toBeUndefined();
	await expect.poll(count(page.getByTestId("workflow-row"))).toBe(1);
	expect(dialogs).toEqual([]);
});

test("the workflow editor analyzes the prompt as you type, with the trigger message in the estimate", async () => {
	const { calls } = mockWorkflows([TRIAGE]);
	await renderApp(WORKFLOWS_TAB);
	await page.getByTestId("workflow-add").click();
	const form = page.getByTestId("workflow-form");
	const feedback = form.getByTestId("task-cost-feedback");
	expect(feedback.elements()).toHaveLength(0);
	// The same section as the task editors: prompt, analysis, precompile, model.
	await expect
		.element(form.getByTestId("workflow-prompt-section"))
		.toBeVisible();
	await expect.element(form.getByTestId("workflow-model")).toBeVisible();
	const precompile = form.getByLabelText("Try to precompile");
	await expect.element(precompile).toBeChecked();
	await expect
		.element(form.getByTestId("workflow-precompile-help"))
		.toMatchTextContent("runs on each matching message without the model");

	// pressSequentially with a 20ms delay between keys.
	await page.getByTestId("workflow-content").click();
	for (const ch of "Summarize the crash") {
		await commands.keyType(ch);
		await sleep(20);
	}
	await expect.element(feedback).toHaveAttribute("data-state", "ready");
	// Rapid typing is analyzed once, as a workflow. (The agent settings
	// page behind the tab analyzes its own selected task.)
	const typed = (calls.task_analyze ?? []).filter((c) =>
		String(c.content).startsWith("Summ"),
	);
	expect(typed).toHaveLength(1);
	expect(typed[0]).toMatchObject({
		agent: "assistant",
		content: "Summarize the crash",
		type: "prompt",
		trigger: "message",
		precompile: true,
	});
	expect(String(typed[0].editor)).toMatch(/^task-editor-/);
	await expect
		.element(feedback.getByTestId("task-cost-verdict"))
		.toHaveText("Not deterministic");
	await expect
		.element(feedback.getByTestId("task-cost-estimate"))
		.toHaveText("≈ 5k–9k tokens/run ($0.010–$0.030)");
	await expect
		.element(feedback.getByTestId("task-cost-context"))
		.toHaveText(
			"Includes the matched message and the messages before it, ≈ 55–1.2k tokens each model call.",
		);

	// A deterministic prompt is precompiled, with the model's cost beside it.
	await page.getByTestId("workflow-content").fill("Reply with the crash ID");
	await expect
		.element(feedback.getByTestId("task-cost-verdict"))
		.toHaveText("This task can run deterministically");
	await expect
		.element(feedback.getByTestId("task-cost-estimate"))
		.toHaveText("≈ 0 tokens/run");
	await expect
		.element(feedback.getByTestId("task-cost-without-precompile"))
		.toHaveText("≈ 5k–9k tokens/run ($0.010–$0.030) without precompile");
	await expect
		.element(feedback.getByTestId("task-cost-note"))
		.toMatchTextContent(
			"runs on each matching message with no model calls; a run whose script fails falls back to the model",
		);

	// A script workflow is already a script.
	await chooseOption(form.getByRole("combobox", { name: "Type" }), "script");
	await expect.element(precompile).toBeDisabled();
	await expect
		.element(feedback.getByTestId("task-cost-estimate"))
		.toHaveText("0 tokens/run");
});

test("a workflow's precompile choice is saved and only sent again when it changes", async () => {
	const { calls } = mockWorkflows([
		{ ...TRIAGE, precompile: true, compiled: 'print("ok")', compiled_key: "k" },
	]);
	await renderApp(WORKFLOWS_TAB);
	await expect
		.element(
			page.getByTestId("workflow-row").getByTestId("compiled-status-chip"),
		)
		.toHaveText("Compiled");

	// A new workflow precompiles by default.
	await page.getByTestId("workflow-add").click();
	await page.getByTestId("workflow-name").fill("crash-ids");
	await page.getByTestId("workflow-content").fill("Reply with the crash ID");
	await page.getByTestId("workflow-save").click();
	await expect.poll(count(page.getByTestId("workflow-row"))).toBe(2);
	expect(calls.task_schedule?.[0]).toMatchObject({
		name: "crash-ids",
		type: "prompt",
		precompile: true,
	});

	// Editing a compiled workflow says so, and leaves an unchanged choice alone.
	await editButton("triage").click();
	const precompile = page
		.getByTestId("workflow-form")
		.getByLabelText("Try to precompile");
	await expect.element(precompile).toBeChecked();
	await expect
		.element(page.getByTestId("workflow-precompile-help"))
		.toMatchTextContent("Precompiled: each matching message runs its script");
	await page.getByTestId("workflow-save").click();
	await expect.poll(count(page.getByTestId("workflow-form"))).toBe(0);
	expect(calls.task_update?.[0]).not.toHaveProperty("precompile");

	await editButton("triage").click();
	await setChecked(precompile, false);
	await page.getByTestId("workflow-save").click();
	await expect.poll(count(page.getByTestId("workflow-form"))).toBe(0);
	expect(calls.task_update?.[1]).toMatchObject({
		name: "assistant/triage",
		precompile: false,
	});
});
