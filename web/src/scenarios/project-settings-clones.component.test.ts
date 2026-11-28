// Code Loop settings: clones, the instance ID, job reports and deleting
// projects, ported from web/e2e/project-settings.spec.ts (#1636).
import { expect, test } from "vitest";
import { page } from "vitest/browser";
import { goto, renderApp } from "../component-test/app";
import {
	chooseOption,
	chooseValue,
	expectValue,
	optionLabels,
} from "./helpers/select";
import { systemList } from "./helpers/sidebar";
import {
	CLONES,
	CONFIG,
	count,
	expectHidden,
	REPORTS_CONFIG,
	saveChanges,
	savedProjects,
	setChecked,
	setup,
} from "./project-settings.fixtures";
// The pages this file renders (see component-test/routes.ts).
import "../views/SettingsView.vue";

test("clones show what's in use and unused ones can be cleared", async () => {
	const calls = setup();
	await renderApp("/projects/settings");
	const rows = page.getByTestId("clone-row");
	await expect.poll(() => count(rows)).toBe(3);
	await expect.element(rows.first()).toMatchTextContent("lsegal/aviary#209");
	await expect
		.element(rows.first().getByTestId("clone-state"))
		.toHaveText("unused: not owned by this instance: no job here records it");
	await expect.element(rows.nth(1)).toMatchTextContent("PR #233");
	await expect
		.element(rows.nth(1).getByTestId("clone-state"))
		.toHaveText("in use: job is paused");
	await expect
		.element(rows.nth(2).getByTestId("clone-state"))
		.toHaveText(
			"kept: outside the configured temp directories; remove it by hand",
		);
	await expect
		.element(rows.first().getByTestId("clone-size"))
		.toHaveText("150 MB");
	await expect
		.element(rows.nth(1).getByTestId("clone-size"))
		.toHaveText("size unknown");
	await expect
		.element(page.getByTestId("clone-total"))
		.toHaveText("3 clones, 150 MB total (1 not measured)");

	await page.getByRole("button", { name: "Clear unused clones" }).click();
	await expect
		.element(page.getByTestId("clone-message"))
		.toHaveText("Removed 1 clone, reclaimed 150 MB, kept 2.");
	expect(calls.project_clone_clear).toEqual([{}]);
});

test("a long clone list scrolls inside the Clones card instead of growing it", async () => {
	const clones = Array.from({ length: 40 }, (_, i) => ({
		path: `/tmp/aviary-${300 + i}`,
		repository: "lsegal/aviary",
		issue: 300 + i,
		inUse: false,
		reason: "job finished",
		size: 1048576,
	}));
	setup({
		clones: { ...CLONES, clones, totalSize: 40 * 1048576, unmeasured: 0 },
	});
	await renderApp("/projects/settings");
	const list = page.getByTestId("clone-list");
	await expect.poll(() => count(page.getByTestId("clone-row"))).toBe(40);
	const el = list.element() as HTMLElement;
	expect(getComputedStyle(el).overflowY).toBe("auto");
	expect(el.clientHeight).toBeLessThanOrEqual(384);
	expect(el.scrollHeight).toBeGreaterThan(el.clientHeight);
	await expect
		.element(page.getByTestId("clone-total"))
		.toHaveText("40 clones, 40 MB total");
});

test("a short clone list does not scroll", async () => {
	setup();
	await renderApp("/projects/settings");
	const list = page.getByTestId("clone-list");
	await expect.poll(() => count(page.getByTestId("clone-row"))).toBe(3);
	const el = list.element() as HTMLElement;
	expect(el.scrollHeight).toBe(el.clientHeight);
});

test("Code Loop settings live under the projects section, not System", async () => {
	setup();
	await renderApp("/projects/settings");
	await expect
		.element(page.getByTestId("settings-title"))
		.toHaveText("Settings");
	await expect.element(page.getByTestId("project-settings")).toBeVisible();
	const system = await systemList();
	await expect
		.poll(() =>
			count(system.getByRole("link", { name: "Projects", exact: true })),
		)
		.toBe(0);
	await goto("/system/projects");
	await expect.poll(() => count(page.getByTestId("project-settings"))).toBe(0);
});

test("Code Loop settings shows this instance's ID", async () => {
	setup();
	await renderApp("/projects/settings");
	const card = page.getByTestId("projects-instance-id");
	await expect.element(card).toMatchTextContent("Instance ID");
	await expect.element(card.locator("code").first()).toHaveText("7F3CA91B");
	await expect.element(card).toMatchTextContent("/aviary:7F3CA91B");
});

test("an unreadable work state shows an error instead of clones", async () => {
	setup({
		clonesError: "work state could not be read: unexpected end of JSON",
	});
	await renderApp("/projects/settings");
	await expect
		.element(
			page.getByText(
				"Couldn't list clones: work state could not be read: unexpected end of JSON",
			),
		)
		.toBeVisible();
	await expect.poll(() => count(page.getByTestId("clone-row"))).toBe(0);
});

test("reports can be turned on with an agent and a target, then off", async () => {
	const calls = setup({
		config: REPORTS_CONFIG,
		sessions: [
			{ id: "sess_main", agent_id: "atlas", name: "main", created_at: "" },
			{
				id: "sess_reports",
				agent_id: "atlas",
				name: "sess_reports",
				title: "Project reports",
				created_at: "",
			},
		],
	});
	await renderApp("/projects/settings?agent=coder");
	const yaml = page.getByTestId("agent-project-yaml");
	const toggle = page.getByRole("switch", { name: "Send job reports" });
	const reportsAgent = page.locator('[data-field="reports_agent"]');
	await expect.element(toggle).toHaveAttribute("aria-checked", "false");
	await expect.poll(() => count(reportsAgent)).toBe(0);

	await toggle.click();
	// Reports go through the owning agent unless another is chosen.
	await expectValue(reportsAgent, "coder");
	await expect
		.element(page.getByTestId("reports-error"))
		.toMatchTextContent("Reports need at least one target.");
	await chooseValue(reportsAgent, "atlas");

	// A channel is picked by name: the report lands in its session.
	await chooseOption(
		page.getByRole("combobox", { name: "Target 1 channel" }),
		"Channel: Team Slack",
	);
	await expect
		.poll(() =>
			count(page.getByRole("textbox", { name: "Target 1 delivery ID" })),
		)
		.toBe(0);
	await page.getByRole("button", { name: "Add a target" }).click();
	await chooseValue(
		page.getByRole("combobox", { name: "Target 2 channel" }),
		"session",
	);
	// A session target is picked by name and saved by its ID.
	const session = page.getByRole("combobox", { name: "Target 2 session" });
	expect(await optionLabels(session)).toEqual(["Main Chat", "Project reports"]);
	await chooseOption(session, "Project reports");
	await setChecked(page.locator('[data-field="report_waiting"]'), false);
	await expect.poll(() => count(page.getByTestId("reports-error"))).toBe(0);
	await expect
		.element(yaml)
		.toMatchTextContent(
			[
				"      - target: lsegal/aviary",
				"        other: kept",
				"        reports:",
				"          agent: atlas",
				'          targets: ["channel:Team Slack", "session:sess_reports"]',
				"          events: [merged, failed]",
			]
				.join(" ")
				.replace(/\s+/g, " ")
				.trim(),
		);

	await saveChanges();
	await expect.poll(() => calls.config_save?.length).toBe(1);
	expect(savedProjects(calls, "coder")).toEqual([
		{
			target: "lsegal/aviary",
			other: "kept",
			reports: {
				agent: "atlas",
				targets: ["channel:Team Slack", "session:sess_reports"],
				events: ["merged", "failed"],
			},
		},
	]);

	// Turning reports off removes the key again.
	await toggle.click();
	await expect.element(yaml).not.toMatchTextContent("reports:");
	await saveChanges();
	await expect.poll(() => calls.config_save?.length).toBe(2);
	expect(savedProjects(calls, "coder")).toEqual(
		REPORTS_CONFIG.agents[2].projects,
	);
});

test("saved reports load into the form and are checked like the server does", async () => {
	setup({
		config: {
			...REPORTS_CONFIG,
			agents: [
				REPORTS_CONFIG.agents[0],
				{
					name: "coder",
					model: "cli/claude/opus",
					projects: [
						{
							target: "lsegal/aviary",
							reports: { agent: "atlas", targets: ["channel:Team Slack"] },
						},
					],
				},
			],
		},
	});
	await renderApp("/projects/settings?agent=coder");
	await expect
		.element(page.getByRole("switch", { name: "Send job reports" }))
		.toHaveAttribute("aria-checked", "true");
	await expectValue(page.locator('[data-field="reports_agent"]'), "atlas");
	await expectValue(
		page.getByRole("combobox", { name: "Target 1 channel" }),
		"channel:Team Slack",
	);
	// No events filter means every event is reported.
	for (const e of ["merged", "failed", "waiting"])
		await expect
			.element(page.locator(`[data-field="report_${e}"]`))
			.toBeChecked();

	for (const e of ["merged", "failed", "waiting"])
		await setChecked(page.locator(`[data-field="report_${e}"]`), false);
	await expect
		.element(page.getByTestId("reports-error"))
		.toMatchTextContent("Choose at least one event to report.");
	await setChecked(page.locator('[data-field="report_failed"]'));
	await page.getByRole("button", { name: "Remove target 1" }).click();
	await expect
		.element(page.getByTestId("reports-error"))
		.toMatchTextContent("Reports need at least one target.");
});

/** CONFIG with coder owning a second project, lsegal/nupp. */
const TWO_PROJECTS_CONFIG = {
	...CONFIG,
	agents: [
		CONFIG.agents[0],
		{
			...CONFIG.agents[1],
			projects: [{ target: "lsegal/aviary" }, { target: "lsegal/nupp" }],
		},
	],
};

test("a project row's ✕ deletes that project after a confirmation naming it and its in-flight jobs", async () => {
	const calls = setup({
		config: TWO_PROJECTS_CONFIG,
		jobs: [
			{ number: 12, status: "active" },
			{ number: 14, status: "paused" },
			{ number: 15, status: "complete" },
		],
	});
	await renderApp("/projects/settings?agent=coder");
	const projects = page.getByTestId("agent-project");
	await expect.poll(() => count(projects)).toBe(2);
	// Every saved row carries its own ✕; there is no separate Delete section.
	await expect
		.element(page.getByRole("button", { name: "Delete lsegal/aviary" }))
		.toBeVisible();
	await expect.poll(() => count(page.locator("#delete"))).toBe(0);
	await expect
		.poll(() => count(page.getByRole("button", { name: "Delete project" })))
		.toBe(0);
	// A saved project is deleted with a confirmation, not dropped from the draft.
	await expect
		.poll(() => count(page.getByRole("button", { name: "Remove lsegal/nupp" })))
		.toBe(0);

	// The ✕ deletes its own row's project, not the selected one.
	await expect
		.element(page.getByRole("button", { name: "lsegal/aviary", exact: true }))
		.toHaveAttribute("aria-pressed", "true");
	await page.getByRole("button", { name: "Delete lsegal/nupp" }).click();
	const dialog = page.getByRole("alertdialog");
	await expect
		.element(dialog.getByRole("heading"))
		.toHaveText("Delete lsegal/nupp?");
	await expect
		.element(dialog)
		.toMatchTextContent("its issues and pull requests are not touched");
	await expect
		.element(dialog.getByTestId("delete-jobs"))
		.toMatchTextContent(
			"2 jobs are still in flight (#12, #14). Deleting stops them",
		);
	expect(calls.project_job_list?.at(-1)).toMatchObject({
		target: "lsegal/nupp",
	});

	// Cancelling removes nothing.
	await dialog.getByRole("button", { name: "Cancel" }).click();
	await expectHidden(dialog);
	expect(calls.project_remove).toBeUndefined();

	await page.getByRole("button", { name: "Delete lsegal/nupp" }).click();
	await dialog.getByRole("button", { name: "Delete project" }).click();
	await expectHidden(dialog);
	await expect.poll(() => count(projects)).toBe(1);
	await expect
		.element(projects.first())
		.toHaveAttribute("data-target", "lsegal/aviary");
	expect(calls.project_remove).toEqual([
		{ target: "lsegal/nupp", delete_data: true },
	]);
	// The removal goes through project_remove alone, never a config rewrite.
	expect(calls.config_save).toBeUndefined();
});

test("a failed project delete keeps the confirmation open with the error", async () => {
	const calls = setup({
		config: TWO_PROJECTS_CONFIG,
		removeError: "project is locked",
	});
	await renderApp("/projects/settings?agent=coder");
	await page.getByRole("button", { name: "Delete lsegal/nupp" }).click();
	const dialog = page.getByTestId("delete-project-dialog");
	await dialog.getByRole("button", { name: "Delete project" }).click();
	await expect
		.element(dialog.getByTestId("delete-project-dialog-error"))
		.toMatchTextContent("project is locked");
	await expect.element(dialog).toBeVisible();
	await expect.poll(() => count(page.getByTestId("agent-project"))).toBe(2);
	expect(calls.project_remove).toEqual([
		{ target: "lsegal/nupp", delete_data: true },
	]);

	await dialog.getByRole("button", { name: "Cancel" }).click();
	await expectHidden(dialog);
});

test("deleting a project offers to delete its data, checked by default", async () => {
	const calls = setup({ config: TWO_PROJECTS_CONFIG });
	await renderApp("/projects/settings?agent=coder");
	await page.getByRole("button", { name: "Delete lsegal/nupp" }).click();
	const dialog = page.getByTestId("delete-project-dialog");
	const option = dialog.getByTestId("delete-project-data");
	await expect
		.element(option)
		.toMatchTextContent("Delete session data and files");
	await expect
		.element(option)
		.toMatchTextContent(
			"Its job records, job transcripts, and the clones no job still uses.",
		);
	const checkbox = option.getByTestId("delete-project-data-checkbox");
	await expect.element(checkbox).toBeChecked();
	const summary = option.getByTestId("delete-project-data-summary");
	await expect
		.element(summary)
		.toHaveText("Deletes 12 runs, 5 files. This can't be undone.");
	expect(calls.delete_preview?.at(-1)).toMatchObject({
		kind: "project",
		name: "lsegal/nupp",
	});

	await setChecked(checkbox, false);
	await expect
		.element(summary)
		.toHaveText("Keeps its job records, transcripts and clones on disk.");
	await dialog.getByRole("button", { name: "Delete project" }).click();
	await expectHidden(dialog);
	expect(calls.project_remove).toEqual([
		{ target: "lsegal/nupp", delete_data: false },
	]);

	// The next delete starts checked again.
	await page.getByRole("button", { name: "Delete lsegal/aviary" }).click();
	await expect.element(checkbox).toBeChecked();
});
