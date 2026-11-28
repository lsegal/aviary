// Ported from web/e2e/projects.spec.ts (#1636): the Code Loop entries in the
// sidebar, rail and phone tab bar, and a board's project-URL header link.
import { afterEach, describe, expect, test, vi } from "vitest";
import { commands, type Locator, page } from "vitest/browser";
import { currentURL, goto, reload, renderApp } from "../component-test/app";
import { boundingBoxOf } from "./helpers/box";
import { JOBS, STATUS, setup } from "./projects-fixtures";

afterEach(() => {
	vi.useRealTimers();
});

/** Fakes the poll timers, as page.clock.install, letting real time run. */
function installClock() {
	vi.useFakeTimers({
		toFake: ["setTimeout", "clearTimeout", "setInterval", "clearInterval"],
		shouldAdvanceTime: true,
	});
}

const opacityOf = (loc: Locator) =>
	Number(getComputedStyle(loc.element()).opacity);

test("an agent's Code Loop in the sidebar list its projects with live counts", async () => {
	setup();
	await renderApp("/system/overview");
	// coder is the only agent, so its pages, Code Loop among them, are
	// the sidebar's top level (#538).
	const row = page.getByTestId("sidebar-row-projects");
	// Counted as the board's Running state counts: #221, with no clone yet,
	// is Starting (#1359).
	await expect
		.element(row.getByTestId("project-running-badge"))
		.toHaveText("1");
	await expect
		.element(
			page.getByTestId("sidebar-nav").getByRole("link", { name: /^Code Loop/ }),
		)
		.toHaveAttribute("href", "/agents/coder/projects");

	await goto("/agents/coder/projects/lsegal%2Faviary");
	const projects = page.getByTestId("sidebar-agent-projects");
	// A project with running jobs shows only their count in a gray badge
	// (#636).
	const aviary = projects.getByRole("link", { name: /lsegal\/aviary/ });
	const badge = aviary.getByTestId("project-running-badge");
	await expect.element(badge).toHaveText("1");
	await expect.element(badge).toHaveAttribute("aria-label", "1 running job");
	await expect.element(badge).toHaveAttribute("title", "1 running job");
	await expect.element(badge).toHaveClass(/\bbg-gray-200\b/);
	await expect.element(aviary).not.toHaveTextContent("running");
	// An idle project shows nothing beside its name.
	const zvidlib = projects.getByRole("link", { name: /lsegal\/zvidlib/ });
	await expect.element(zvidlib).toHaveText("lsegal/zvidlib");
	await expect
		.poll(() => zvidlib.getByTestId("project-running-badge").elements().length)
		.toBe(0);
	await expect.element(zvidlib).not.toHaveTextContent("idle");
	await expect
		.element(page.getByTestId("sidebar-add-project"))
		.toHaveAttribute("href", "/agents/coder/projects?add=1");
});

test("the sidebar always shows Code Loop expanded with its projects (#867)", async () => {
	setup();
	// Off the Code Loop pages, Code Loop still lists every project.
	await renderApp("/system/overview");
	const projects = page.getByTestId("sidebar-agent-projects");
	await expect.element(projects).toBeVisible();
	await expect
		.element(projects.getByRole("link", { name: /lsegal\/aviary/ }))
		.toHaveAttribute("href", "/agents/coder/projects/lsegal%2Faviary");
	await expect
		.element(projects.getByRole("link", { name: /lsegal\/zvidlib/ }))
		.toBeVisible();

	// Reloading, and leaving the board and coming back, keep it expanded.
	const nav = page.getByTestId("sidebar-nav");
	await nav.getByRole("link", { name: /^Code Loop/ }).click();
	await expect.poll(currentURL).toMatch(/\/agents\/coder\/projects$/);
	await expect.element(projects).toBeVisible();
	await nav.getByRole("link", { name: "Agent Settings" }).click();
	await expect.poll(currentURL).toMatch(/\/agents\/coder\/settings/);
	await expect.element(projects).toBeVisible();
	await reload();
	await expect
		.element(projects.getByRole("link", { name: /lsegal\/aviary/ }), {
			timeout: 15_000,
		})
		.toBeVisible();
});

test('the Code Loop running badge is gray and the Add project "+" sits beside it on hover', async () => {
	setup();
	await renderApp("/system/overview");
	const row = page.getByTestId("sidebar-row-projects");
	const badge = row.getByTestId("project-running-badge");
	await expect.element(badge).toHaveText("1");
	await expect.element(badge).toHaveAttribute("aria-label", "1 running job");
	await expect.element(badge).toHaveClass(/\bbg-gray-200\b/);
	await expect
		.poll(() => row.getByTestId("agent-running-badge").elements().length)
		.toBe(0);

	const plus = page.getByTestId("sidebar-add-project");
	await expect.element(plus).toHaveAttribute("aria-label", "Add project");
	await expect.poll(() => opacityOf(plus)).toBe(0);
	await page
		.getByTestId("sidebar-nav")
		.getByRole("link", { name: /^Code Loop/ })
		.hover();
	await expect.poll(() => opacityOf(plus)).toBe(1);

	const badgeBox = await boundingBoxOf(badge);
	const plusBox = await boundingBoxOf(plus);
	expect(badgeBox.x + badgeBox.width).toBeLessThanOrEqual(plusBox.x);
});

test('Code Loop badges sit at the row\'s right edge until the "+" shows', async () => {
	setup();
	await renderApp("/agents/coder/projects/lsegal%2Faviary");
	await commands.mouseMove(800, 600);
	// Gap between a badge's right edge and its row's right edge.
	const gap = async (row: Locator, badge: Locator) => {
		const r = await boundingBoxOf(row);
		const b = await boundingBoxOf(badge);
		return r.x + r.width - (b.x + b.width);
	};
	const header = page
		.getByTestId("sidebar-nav")
		.getByRole("link", { name: /^Code Loop/ });
	const headerBadge = header.getByTestId("project-running-badge");
	const project = page
		.getByTestId("sidebar-agent-projects")
		.getByRole("link", { name: /lsegal\/aviary/ });
	const projectBadge = project.getByTestId("project-running-badge");
	// With nothing hovered both badges hug the right edge, inside the row's
	// own padding.
	await expect.poll(() => gap(header, headerBadge)).toBeLessThanOrEqual(12);
	await expect.poll(() => gap(project, projectBadge)).toBeLessThanOrEqual(12);

	// Hovering shows the "+", and the badge moves left of it.
	const plus = page.getByTestId("sidebar-add-project");
	await header.hover();
	await expect.poll(() => opacityOf(plus)).toBe(1);
	await expect.poll(() => gap(header, headerBadge)).toBeGreaterThan(24);
	const badgeBox = await boundingBoxOf(headerBadge);
	const plusBox = await boundingBoxOf(plus);
	expect(badgeBox.x + badgeBox.width).toBeLessThanOrEqual(plusBox.x);
});

test("a project's running badge follows its jobs live in the sidebar", async () => {
	installClock();
	let running = 1;
	setup(() => STATUS, {
		project_job_list: () => {
			const jobs = Array.from({ length: running }, (_, i) => ({
				...JOBS[0],
				target: "lsegal/zvidlib",
				number: 900 + i,
				status: "active",
			}));
			return { jobs, total: jobs.length, offset: 0, limit: 50 };
		},
	});
	await renderApp("/agents/coder/projects/lsegal%2Fzvidlib");
	const row = page
		.getByTestId("sidebar-agent-projects")
		.getByRole("link", { name: /lsegal\/zvidlib/ });
	const badge = row.getByTestId("project-running-badge");
	await expect.element(badge).toHaveText("1");
	await expect.element(badge).toHaveAttribute("aria-label", "1 running job");

	running = 3;
	await vi.advanceTimersByTimeAsync(10_000);
	await expect.element(badge).toHaveText("3");
	await expect.element(badge).toHaveAttribute("aria-label", "3 running jobs");

	running = 0;
	await vi.advanceTimersByTimeAsync(10_000);
	await expect.poll(() => badge.elements().length).toBe(0);
	await expect.element(row).toHaveText("lsegal/zvidlib");
});

// One watched project with no running jobs shows no badge on the GitHub
// Projects row, not the number of watched projects (#701).
function setupOneProject(running: () => number) {
	setup(() => ({ ...STATUS, targets: ["lsegal/aviary"] }), {
		config_get: {
			agents: [
				{
					name: "coder",
					model: "cli/claude/opus",
					projects: [{ target: "lsegal/aviary" }],
				},
			],
		},
		project_job_list: () => {
			const jobs = Array.from({ length: running() }, (_, i) => ({
				...JOBS[0],
				number: 900 + i,
				status: "active",
			}));
			return { jobs, total: jobs.length, offset: 0, limit: 50 };
		},
	});
}

test("the Code Loop row counts running jobs, not watched projects", async () => {
	installClock();
	let running = 0;
	setupOneProject(() => running);
	await renderApp("/system/overview");
	const row = page
		.getByTestId("sidebar-row-projects")
		.getByRole("link", { name: /^Code Loop/ });
	const badge = row.getByTestId("project-running-badge");
	await expect.element(row).toHaveText("Code Loop");
	await expect.poll(() => badge.elements().length).toBe(0);

	running = 1;
	await vi.advanceTimersByTimeAsync(10_000);
	await expect.element(badge).toHaveText("1");

	running = 0;
	await vi.advanceTimersByTimeAsync(10_000);
	await expect.poll(() => badge.elements().length).toBe(0);
	await expect.element(row).toHaveText("Code Loop");
});

// The collapsed sidebar rail keeps the running count on its Code Loop
// icon (#724).
test("the collapsed rail's Code Loop icon counts running jobs", async () => {
	installClock();
	let running = 2;
	setupOneProject(() => running);
	await renderApp("/system/overview");
	await page.getByTestId("sidebar-collapse").click();
	const icon = page.getByTestId("sidebar-rail-projects");
	// Code Loop's own loop-arrow-around-</> icon, not a generic grid (#1033).
	await expect.element(icon.getByTestId("code-loop-icon")).toBeVisible();
	await expect.element(icon.getByTestId("sidebar-rail-badge")).toHaveText("2");
	await icon.hover();
	await expect
		.element(page.getByRole("tooltip"))
		.toHaveText("Code Loop · 2 running jobs");

	running = 0;
	await vi.advanceTimersByTimeAsync(10_000);
	await expect
		.poll(() => icon.getByTestId("sidebar-rail-badge").elements().length)
		.toBe(0);
});

describe("on a phone", () => {
	test("the Code Loop tab counts running jobs, not watched projects", async () => {
		await page.viewport(390, 844);
		installClock();
		let running = 0;
		setupOneProject(() => running);
		await renderApp("/system/overview");
		const tab = page
			.getByRole("navigation", { name: "Main" })
			.getByRole("link", { name: /^Code Loop/ });
		const badge = tab.getByTestId("project-running-badge");
		await expect.element(tab).toHaveText("Code Loop");
		await expect.poll(() => badge.elements().length).toBe(0);

		running = 1;
		await vi.advanceTimersByTimeAsync(10_000);
		await expect.element(badge).toHaveText("1");

		running = 0;
		await vi.advanceTimersByTimeAsync(10_000);
		await expect.poll(() => badge.elements().length).toBe(0);
		await expect.element(tab).toHaveText("Code Loop");
	});
});

test("a board watching a project URL links its header to that URL", async () => {
	setup();
	const board = "https://github.com/users/lsegal/projects/3";
	await renderApp(`/agents/coder/projects/${encodeURIComponent(board)}`);

	const link = page
		.getByRole("heading", { name: board })
		.getByRole("link", { name: board });
	await expect.element(link).toHaveAttribute("href", board);
	await expect.element(link).toHaveAttribute("target", "_blank");
});
