// Adding a project (the Watch a repository dialog and its checks), ported
// from web/e2e/project-settings.spec.ts (#1636). The rest of Code Loop
// settings lives in project-settings-shared and project-settings-clones.
import { expect, test } from "vitest";
import { page } from "vitest/browser";
import { currentURL, renderApp } from "../component-test/app";
import {
	type Args,
	CODER_PROJECTS,
	CONFIG,
	count,
	expectHidden,
	geminiSkillCheck,
	READY_CHECK,
	saveChanges,
	savedProjects,
	setChecked,
	setup,
} from "./project-settings.fixtures";
// The pages this file renders (see component-test/routes.ts).
import "../views/ProjectsView.vue";
import "../views/SettingsView.vue";

test('the Add project "+" on Code Loop opens Add project', async () => {
	setup();
	await renderApp("/agents/coder/projects/lsegal%2Faviary");
	await page
		.getByTestId("sidebar-nav")
		.getByRole("link", { name: /^Code Loop/ })
		.hover();
	await page.getByRole("link", { name: "Add project", exact: true }).click();
	await expect.poll(currentURL).toBe("/agents/coder/projects?add=1");
	await expect
		.element(page.getByRole("dialog", { name: "Watch a repository" }))
		.toBeVisible();
});

test("adding a project from the single field saves it to the board's agent", async () => {
	const calls = setup();
	await renderApp("/agents/coder/projects/lsegal%2Faviary?add=1");

	const dialog = page.getByRole("dialog", { name: "Watch a repository" });
	await expect.element(dialog).toBeVisible();
	await dialog
		.getByLabelText("Repository or project board")
		.fill("https://github.com/lsegal/zvidlib");

	const checks = dialog.getByTestId("add-project-checks");
	await expect.element(checks).toMatchTextContent("Ready to go");
	await expect
		.element(checks)
		.toMatchTextContent("gh is signed in and can read lsegal/zvidlib");
	await expect
		.element(checks)
		.toMatchTextContent("pull requests will come from your fork");
	await expect
		.element(checks.locator('[data-check="fork"]'))
		.toHaveAttribute("data-status", "warn");
	// The board's agent owns it; the dialog offers no other owner.
	await expect
		.poll(() => count(dialog.getByLabelText("Agent that owns it")))
		.toBe(0);
	await expect.poll(() => count(dialog.getByRole("combobox"))).toBe(0);
	await expect
		.element(dialog)
		.toMatchTextContent("Runs on coder: claude/opus, codex/gpt-5.5");
	await expect.element(dialog).toMatchTextContent("Up to 3 jobs at once");
	await expect
		.element(dialog.locator("footer code"))
		.toHaveText("aviary project add lsegal/zvidlib --agent coder");
	await expect
		.element(dialog.locator("footer"))
		.toMatchTextContent("saved to coder's projects");

	await dialog.getByRole("button", { name: "Start watching" }).click();
	await expect.poll(currentURL).toBe("/agents/coder/projects/lsegal%2Fzvidlib");
	await expectHidden(dialog);
	expect(calls.project_add).toEqual([
		{ target: "lsegal/zvidlib", agent: "coder" },
	]);
	// The board's agent and its models pick the CLIs whose skills count.
	expect(calls.project_check?.at(-1)).toEqual({
		target: "https://github.com/lsegal/zvidlib",
		agent: "coder",
		models: ["cli/claude/opus", "cli/codex/gpt-5.5"],
	});
});

test("a project can be added in manual mode", async () => {
	const calls = setup();
	await renderApp("/agents/coder/projects/lsegal%2Faviary?add=1");
	const dialog = page.getByRole("dialog", { name: "Watch a repository" });
	await dialog
		.getByLabelText("Repository or project board")
		.fill("lsegal/zvidlib");
	// Only Watch and Manual are offered, with the global mode picked (#1461).
	const modes = dialog.getByTestId("add-project-mode");
	await expect.poll(() => count(modes.getByRole("radio"))).toBe(2);
	await expect.element(modes).not.toMatchTextContent(/inherit/i);
	await expect
		.element(dialog.getByRole("radio", { name: /Watch issues/ }))
		.toBeChecked();
	const footerCode = dialog.locator("footer code");
	await expect
		.element(footerCode)
		.toHaveText("aviary project add lsegal/zvidlib --agent coder");
	await setChecked(
		dialog.getByRole("radio", { name: /Manual: only what I queue/ }),
	);
	await expect
		.element(dialog)
		.toMatchTextContent("Issues are never picked up on their own");
	await expect
		.element(footerCode)
		.toHaveText(
			"aviary project add lsegal/zvidlib --agent coder --mode manual",
		);
	await expect
		.element(dialog.getByTestId("add-project-checks"))
		.toMatchTextContent("Ready to go");
	await expect.poll(() => calls.project_check?.at(-1)?.mode).toBe("manual");
	// Back to Watch, the global mode, passes no mode at all.
	await setChecked(dialog.getByRole("radio", { name: /Watch issues/ }));
	await expect
		.element(footerCode)
		.toHaveText("aviary project add lsegal/zvidlib --agent coder");
	await setChecked(
		dialog.getByRole("radio", { name: /Manual: only what I queue/ }),
	);
	await dialog.getByRole("button", { name: "Start watching" }).click();
	await expectHidden(dialog);
	expect(calls.project_add).toEqual([
		{ target: "lsegal/zvidlib", agent: "coder", mode: "manual" },
	]);
});

test("a project another agent owns cannot be added again", async () => {
	const calls = setup({
		check: { ...READY_CHECK, target: "lsegal/aviary", fork: false },
	});
	// Opened from atlas's board, so atlas would own it.
	await renderApp("/agents/atlas/projects?add=1");
	const dialog = page.getByRole("dialog");
	await expect
		.element(dialog)
		.toMatchTextContent("Runs on atlas: aviary/atlas");
	await dialog
		.getByLabelText("Repository or project board")
		.fill("lsegal/aviary");
	await expect
		.element(dialog.getByTestId("add-project-owned"))
		.toHaveText("lsegal/aviary already belongs to agent coder.");
	await expect
		.element(dialog.getByRole("button", { name: "Start watching" }))
		.toBeDisabled();
	expect(calls.project_add).toBeUndefined();
});

test("skills missing on CLIs the project does not use never need attention", async () => {
	setup({ check: geminiSkillCheck });
	await renderApp("/agents/coder/projects/lsegal%2Faviary?add=1");
	const dialog = page.getByRole("dialog");
	await dialog
		.getByLabelText("Repository or project board")
		.fill("lsegal/zvidlib");

	const checks = dialog.getByTestId("add-project-checks");
	await expect.element(checks).toMatchTextContent("Ready to go");
	await expect.element(checks).not.toMatchTextContent("Needs attention");
	await expect
		.poll(() => count(checks.locator('[data-status="fail"]')))
		.toBe(0);
	const note = checks.locator('[data-check="skills_unused"]');
	await expect.element(note).toHaveAttribute("data-status", "info");
	await expect
		.element(note)
		.toHaveText("gh-fix also missing for gemini (not used)");
	await expect.element(note.locator("span")).toHaveClass(/text-gray-500/);
	await expect
		.element(dialog.getByRole("button", { name: "Start watching" }))
		.toBeEnabled();
});

test("a fallback CLI missing a skill needs attention and is named", async () => {
	setup({
		check: geminiSkillCheck,
		config: {
			...CONFIG,
			agents: [
				{
					name: "coder",
					model: "cli/claude/opus",
					fallbacks: ["cli/gemini/gemini-3.5-flash"],
					projects: CODER_PROJECTS,
				},
			],
		},
	});
	await renderApp("/agents/coder/projects/lsegal%2Faviary?add=1");
	const dialog = page.getByRole("dialog");
	await dialog
		.getByLabelText("Repository or project board")
		.fill("lsegal/zvidlib");

	const checks = dialog.getByTestId("add-project-checks");
	await expect.element(checks).toMatchTextContent("Needs attention");
	await expect
		.element(checks.locator('[data-check="skills"]'))
		.toHaveAttribute("data-status", "fail");
	await expect
		.element(checks)
		.toMatchTextContent("gh-fix is not installed for gemini");
});

test("the add-project check follows the agent's models as they change", async () => {
	// The config arrives after the first check, which runs without the
	// agent's models; the server then reports gemini, which the agent never
	// uses, as missing gh-fix.
	const calls = setup({
		configDelay: 2000,
		check: (args?: Args) =>
			geminiSkillCheck({
				...args,
				models: (args?.models as string[] | undefined) ?? ["gemini/m"],
			}),
	});
	await renderApp("/agents/coder/projects/lsegal%2Faviary?add=1");
	const dialog = page.getByRole("dialog");
	await dialog
		.getByLabelText("Repository or project board")
		.fill("lsegal/zvidlib");

	const checks = dialog.getByTestId("add-project-checks");
	await expect.element(checks).toMatchTextContent("Needs attention");
	await expect.element(checks).toMatchTextContent("Ready to go");
	await expect
		.element(checks)
		.not.toMatchTextContent("not installed for gemini");
	expect(calls.project_check?.at(0)?.models).toBeUndefined();
	expect(calls.project_check?.at(-1)?.models).toEqual([
		"cli/claude/opus",
		"cli/codex/gpt-5.5",
	]);
});

test("add-project checks report each failing prerequisite", async () => {
	const calls = setup({
		check: {
			target: "lsegal/zvidlib",
			kind: "repo",
			ok: false,
			checks: [
				{
					name: "github",
					status: "ok",
					message: "gh is signed in and can read lsegal/zvidlib",
				},
				{
					name: "clis",
					status: "fail",
					message:
						"No CLI provider found; install one of claude, codex, gemini, opencode or cline",
				},
				{
					name: "skills",
					status: "fail",
					message: "gh-fix cannot be installed until a CLI provider is found",
				},
			],
		},
	});
	await renderApp("/agents/coder/projects/lsegal%2Faviary?add=1");
	const dialog = page.getByRole("dialog");
	await dialog
		.getByLabelText("Repository or project board")
		.fill("lsegal/zvidlib");

	const checks = dialog.getByTestId("add-project-checks");
	await expect.element(checks).toMatchTextContent("Needs attention");
	await expect
		.poll(() => count(checks.locator('[data-status="fail"]')))
		.toBe(2);
	for (const name of ["clis", "skills"]) {
		await expect
			.element(checks.locator(`[data-check="${name}"]`))
			.toHaveAttribute("data-status", "fail");
	}
	await expect.element(checks).not.toMatchTextContent("label");
	await expect
		.element(checks.getByRole("link", { name: "Manage skills" }))
		.toBeVisible();
	await expect
		.element(dialog.getByRole("button", { name: "Start anyway" }))
		.toBeEnabled();

	await dialog.getByRole("button", { name: "Cancel" }).click();
	await expectHidden(dialog);
	await expect.poll(currentURL).toBe("/agents/coder/projects/lsegal%2Faviary");
	expect(calls.project_add).toBeUndefined();
});

test("change settings first opens the agent's projects with the new one listed", async () => {
	const calls = setup();
	await renderApp("/agents/coder/projects/lsegal%2Faviary?add=1");
	const dialog = page.getByRole("dialog");
	await dialog
		.getByLabelText("Repository or project board")
		.fill("lsegal/zvidlib");
	await expect
		.element(dialog.getByTestId("add-project-checks"))
		.toMatchTextContent("Ready to go");
	await dialog.getByRole("button", { name: "Change settings first" }).click();
	await expect
		.poll(currentURL)
		.toBe("/projects/settings?agent=coder&add=lsegal%2Fzvidlib");
	await expect.poll(() => count(page.getByTestId("agent-project"))).toBe(2);
	await expect
		.element(page.getByTestId("agent-project-yaml"))
		.toMatchTextContent("- target: lsegal/zvidlib");
	await saveChanges();
	await expect.poll(() => calls.config_save?.length).toBe(1);
	expect(savedProjects(calls, "coder")).toEqual([
		{ target: "lsegal/aviary" },
		{ target: "lsegal/zvidlib" },
	]);
	expect(calls.project_add).toBeUndefined();
});

test("a project being added has nothing to delete", async () => {
	setup();
	await renderApp("/projects/settings?agent=coder&add=lsegal%2Fzvidlib");
	await expect.poll(() => count(page.getByTestId("agent-project"))).toBe(2);
	await expect
		.element(page.getByRole("button", { name: "lsegal/zvidlib", exact: true }))
		.toHaveAttribute("aria-pressed", "true");
	await expect
		.poll(() =>
			count(page.getByRole("button", { name: "Delete lsegal/zvidlib" })),
		)
		.toBe(0);
	await expect
		.element(page.getByRole("button", { name: "Remove lsegal/zvidlib" }))
		.toBeVisible();
	await expect
		.element(page.getByRole("button", { name: "Delete lsegal/aviary" }))
		.toBeVisible();
});
