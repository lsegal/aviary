// Ported from web/e2e/projects.spec.ts (#1636): an agent's Code Loop board and job detail actions.
import { afterEach, expect, test, vi } from "vitest";
import { commands, type Locator, page } from "vitest/browser";
import { currentURL, goto, renderApp } from "../component-test/app";
import { inTargets, JOBS, job, setup, stateChip } from "./projects-fixtures";




const ZVID_JOB = job({
	number: 7,
	title: "Decode B-frames",
	status: "active",
	agent: "claude",
	model: "opus",
	started: 5,
	target: "lsegal/zvidlib",
});

test("an agent's Code Loop is one board of all its projects, with no settings", async () => {
	const calls = setup(undefined, {
		project_job_list: (args?: Record<string, unknown>) => {
			calls.project_job_list ??= [];
			calls.project_job_list.push(args ?? {});
			const query = typeof args?.query === "string" ? args.query : "";
			const jobs = [...JOBS, ZVID_JOB].filter(
				(j) =>
					inTargets(args, j.target) &&
					(!args?.status || j.status === args.status) &&
					(!args?.states || (args.states as string[]).includes(j.state)) &&
					(!query || j.title.toLowerCase().includes(query.toLowerCase())),
			);
			return { jobs, total: jobs.length, offset: 0, limit: args?.limit ?? 50 };
		},
	});
	await renderApp("/agents/coder/projects");

	await expect
		.element(page.getByRole("heading", { name: "Code Loop" }))
		.toBeVisible();
	await expect
		.element(page.getByTestId("board-projects"))
		.toHaveText("2 projects");
	// Every project's jobs share the board, each naming its project.
	const cards = page.getByTestId("job-card");
	await expect.poll(() => cards.elements().length).toBe(6);
	// The board's own call names its targets; the project pills' poll reads
	// every project's jobs (#1044).
	expect(
		calls.project_job_list.filter((c) => c.targets).at(-1)?.targets,
	).toEqual(["lsegal/aviary", "lsegal/zvidlib"]);
	const zvid = cards.filter({ hasText: "Decode B-frames" });
	await expect
		.element(zvid.getByTestId("job-project"))
		.toHaveText("lsegal/zvidlib");
	await expect
		.element(zvid.getByTestId("job-project"))
		.toHaveAttribute("href", "/agents/coder/projects/lsegal%2Fzvidlib");

	// No settings live on the route: no form fields, and Settings opens the
	// agent's projects in Code Loop settings.
	await expect
		.poll(() => page.locator("[data-field]").elements().length)
		.toBe(0);
	await expect
		.poll(() => page.getByTestId("agent-projects").elements().length)
		.toBe(0);
	await page.getByTestId("board-more-menu").click();
	await expect
		.element(page.getByTestId("project-settings-link"))
		.toHaveAttribute("href", "/projects/settings?agent=coder");
	await commands.keyPress("Escape");

	// Filters and search narrow the whole board, as on one project's.
	await stateChip("Running").click();
	await expect.poll(() => cards.elements().length).toBe(2);
	await page.getByLabelText("Search jobs").fill("B-frames");
	await expect.poll(() => cards.elements().length).toBe(1);
	await expect.element(cards).toMatchTextContent("lsegal/zvidlib");

	// One check reads every project.
	await page.getByLabelText("Search jobs").fill("");
	await page.getByTestId("board-more-menu").click();
	await page.getByRole("menuitem", { name: "Refresh" }).click();
	await expect.poll(() => calls.project_refresh?.[0]).toEqual({});
});

test("the Code Loop board's header starts with a blue + that adds a project (#739, #1147, #1197)", async () => {
	setup();
	await renderApp("/agents/coder/projects");
	const icons = page.getByTestId("board-add-project").locator("..");
	const add = icons.locator(":scope > *").nth(1);
	await expect.element(add).toHaveAttribute("aria-label", "Add project");
	await expect.element(add).toHaveAttribute("title", "Add project");
	// The sidebar's blue + (#1197): a white + on the same blue.
	const blue = (el: Element) => getComputedStyle(el).backgroundColor;
	await expect
		.poll(() => blue(add.element() as HTMLElement))
		.toBe(
			await blue(
				page.getByTestId("sidebar-add-project").element() as HTMLElement,
			),
		);
	await expect.element(add).toHaveCSS("color", "rgb(255, 255, 255)");
	await expect
		.element(icons.locator(":scope > *").last())
		.toHaveAttribute("data-testid", "board-more-menu");
	await add.click();
	await expect.poll(currentURL).toMatch(/\/agents\/coder\/projects\?add=1$/);
	await expect
		.element(page.getByRole("dialog", { name: "Watch a repository" }))
		.toBeVisible();

	// One project's board has no + in its header.
	await goto("/agents/coder/projects/lsegal%2Faviary");
	await expect.element(page.getByTestId("board-more-menu")).toBeVisible();
	await expect
		.poll(() => page.getByTestId("board-add-project").elements().length)
		.toBe(0);
});

test("one project's board leaves its cards unlabelled", async () => {
	setup();
	await renderApp("/agents/coder/projects/lsegal%2Faviary");
	await expect
		.poll(() => page.getByTestId("job-card").elements().length)
		.toBe(5);
	await expect
		.poll(() => page.getByTestId("job-project").elements().length)
		.toBe(0);
});

test("job actions are icon buttons with tooltips", async () => {
	setup();
	await renderApp("/agents/coder/projects/lsegal%2Faviary");
	const card = page.getByTestId("job-card").filter({ hasText: "#218" });
	const actions = [
		card.getByRole("link", { name: "Details" }),
		card.getByRole("button", { name: "Retry" }),
		card.getByRole("button", { name: "Summarize" }),
		card.getByRole("button", { name: "Stop" }),
	];
	for (const action of actions) {
		await expect.poll(() => action.locator("svg").elements().length).toBe(1);
		await expect.element(action).toHaveText("");
		await expect.element(action).toHaveAttribute("title", /\S/);
	}

	await goto("/agents/coder/projects/lsegal%2Faviary/jobs/218");
	for (const name of ["Retry", "Stop"]) {
		const action = page.getByRole("button", { name });
		await expect.poll(() => action.locator("svg").elements().length).toBe(1);
		await expect.element(action).toHaveText("");
		await expect.element(action).toHaveAttribute("title", /\S/);
	}
});

test("job detail links #NNN and PR #YYY in the title, with icon-only actions (#835)", async () => {
	setup();
	await renderApp("/agents/coder/projects/lsegal%2Faviary/jobs/218");
	const title = page.getByTestId("job-detail-title");
	await expect
		.element(title.getByRole("link", { name: "#218" }))
		.toHaveAttribute("href", "https://github.com/lsegal/aviary/issues/218");
	await expect
		.element(title.getByRole("link", { name: "PR #233" }))
		.toHaveAttribute("href", "https://github.com/lsegal/aviary/pull/233");
	// The header row has no Issue/PR button links left, only icon buttons.
	const actions = page.getByTestId("job-detail-actions");
	await expect
		.poll(() => actions.getByRole("link", { name: /#\d+/ }).elements().length)
		.toBe(0);
	const toggle = page.getByTestId("raw-output-toggle");
	for (const width of [390, 820, 1600]) {
		await page.viewport(width, 900);
		await expect.element(toggle).toHaveText("");
		await expect.poll(() => toggle.locator("svg").elements().length).toBe(1);
		await expect.element(toggle).toHaveAttribute("aria-label", "Raw output");
		await expect.element(toggle).toHaveAttribute("title", /\S/);
	}
	// Closing the details is a plain dismiss icon, not a bordered action.
	expect(
		getComputedStyle(page.getByTestId("job-detail-close").element())
			.borderTopWidth,
	).toBe("0px");

	await goto("/agents/coder/projects/lsegal%2Faviary/jobs/221");
	await expect
		.element(title)
		.toHaveText("#221 Export a session as Markdown from the chat menu");
	await expect.poll(() => title.getByRole("link").elements().length).toBe(1);
});
