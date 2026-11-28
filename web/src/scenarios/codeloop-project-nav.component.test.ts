import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { page } from "vitest/browser";
import { nextTick } from "vue";
import { currentURL, goBack, renderApp } from "../component-test/app";
import {
	SIDEBAR_DEFAULT_WIDTH,
	useSidebarLayout,
} from "../composables/useSidebarLayout";
import {
	backToBoard,
	expectSharedNavOnly,
	job,
	pill,
	setup,
} from "./codeloop-project-nav.fixtures";
// The pages this file renders (see component-test/routes.ts).
import "../views/ProjectsView.vue";
import "../views/SettingsView.vue";

// The Code Loop board's project pills lead into each project and its
// scheduled tasks, so they stay reachable with the sidebar collapsed (#1044).
// The `aviary watch` session cases live in
// codeloop-project-nav-watch.component.test.ts: the signed-out health check
// they load is module state that would carry into the tests after them.

beforeEach(async () => {
	// The sidebar layout lives in module state, which outlasts a test, so
	// start each test from a fresh browser's layout.
	const layout = useSidebarLayout();
	layout.collapsed.value = false;
	layout.width.value = SIDEBAR_DEFAULT_WIDTH;
	await nextTick();
	localStorage.clear();
});

afterEach(() => {
	vi.useRealTimers();
});

test("with the sidebar collapsed, the board's pills open every project and its scheduled tasks", async () => {
	setup(() => [job("lsegal/aviary", 1), job("lsegal/aviary", 2)]);
	await renderApp("/agents/coder/projects");
	await page.getByTestId("sidebar-collapse").click();

	// The pills sit left of the models and quota, in the same row.
	const nav = page.getByRole("navigation", { name: "Projects" });
	await expect
		.poll(() => nav.getByTestId("project-nav-item").elements().length)
		.toBe(2);
	const providers = page.getByRole("region", { name: "Models and quota" });
	await expect.element(providers).toBeVisible();
	const navBox = nav.element().getBoundingClientRect();
	const providersBox = providers.element().getBoundingClientRect();
	expect(navBox.x + navBox.width).toBeLessThanOrEqual(providersBox.x + 1);
	expect(Math.abs(navBox.y - providersBox.y)).toBeLessThan(providersBox.height);

	const aviary = pill("lsegal/aviary");
	await expect
		.element(aviary.getByTestId("project-nav-count"))
		.toHaveText("· 2 running");
	await expect
		.element(aviary.getByTestId("project-nav-dot"))
		.toHaveAttribute("data-state", "running");
	expect(aviary.getByTestId("project-nav-scheduled").elements()).toHaveLength(
		0,
	);
	const forgo = pill("lsegal/forgo");
	await expect
		.element(forgo.getByTestId("project-nav-count"))
		.toHaveText("· 0");
	await expect
		.element(forgo.getByTestId("project-nav-scheduled"))
		.toHaveText("Scheduled 1");

	// A project's pill opens its page, whose breadcrumb leads back.
	await aviary.getByTestId("project-nav-link").click();
	await expect.poll(currentURL).toBe("/agents/coder/projects/lsegal%2Faviary");
	await expect
		.element(page.getByRole("heading", { name: "lsegal/aviary" }))
		.toBeVisible();
	await page
		.getByTestId("settings-breadcrumb")
		.getByRole("link", { name: "Code Loop", exact: true })
		.click();
	await expect.poll(currentURL).toMatch(/\/agents\/coder\/projects(\?.*)?$/);

	// Scheduled opens the project's scheduled tasks.
	await pill("lsegal/forgo").getByTestId("project-nav-scheduled").click();
	await expect
		.poll(currentURL)
		.toBe("/agents/coder/projects/lsegal%2Fforgo?state=scheduled");
	await expect.element(page.getByText("nightly-triage").first()).toBeVisible();
});

test("a project page has no breadcrumb of its own, and the shared one leads back", async () => {
	setup(() => []);
	await renderApp("/agents/coder/projects/lsegal%2Fforgo");
	await expectSharedNavOnly();
	await page
		.getByTestId("settings-breadcrumb")
		.getByRole("link", { name: "Code Loop", exact: true })
		.click();
	await expect.poll(currentURL).toMatch(/\/agents\/coder\/projects(\?.*)?$/);
	await goBack();
	await backToBoard();
});

test("with the sidebar collapsed the shared breadcrumb leads back to the board", async () => {
	setup(() => []);
	await renderApp("/agents/coder/projects/lsegal%2Fforgo");
	await page.getByTestId("sidebar-collapse").click();
	await expect.element(page.getByTestId("sidebar-rail")).toBeVisible();
	await expectSharedNavOnly();
	await backToBoard();
});

test("a project's pill keeps the board's job filter", async () => {
	setup(() => []);
	await renderApp("/agents/coder/projects?state=running");
	await expect
		.element(pill("lsegal/forgo").getByTestId("project-nav-link"))
		.toHaveAttribute(
			"href",
			"/agents/coder/projects/lsegal%2Fforgo?state=running",
		);
});

test("the pills' counts and colors follow jobs live", async () => {
	vi.useFakeTimers({
		toFake: [
			"setTimeout",
			"clearTimeout",
			"setInterval",
			"clearInterval",
			"Date",
		],
		shouldAdvanceTime: true,
	});
	let jobs = [job("lsegal/forgo", 1)];
	setup(() => jobs);
	await renderApp("/agents/coder/projects");
	const forgo = pill("lsegal/forgo");
	await expect
		.element(forgo.getByTestId("project-nav-count"))
		.toHaveText("· 1 running");

	jobs = [job("lsegal/forgo", 1, "paused")];
	await vi.advanceTimersByTimeAsync(5_000);
	await expect
		.element(forgo.getByTestId("project-nav-count"))
		.toHaveText("· 1 waiting");
	await expect
		.element(forgo.getByTestId("project-nav-dot"))
		.toHaveAttribute("data-state", "waiting");

	jobs = [];
	await vi.advanceTimersByTimeAsync(5_000);
	await expect
		.element(forgo.getByTestId("project-nav-count"))
		.toHaveText("· 0");
	await expect
		.element(forgo.getByTestId("project-nav-dot"))
		.toHaveAttribute("data-state", "idle");
});

describe("on a phone", () => {
	test("the pills scroll sideways without overflowing the page", async () => {
		await page.viewport(390, 844);
		setup(() => [job("lsegal/aviary", 1)]);
		await renderApp("/agents/coder/projects");
		const nav = page.getByRole("navigation", { name: "Projects" });
		await expect
			.poll(() => nav.getByTestId("project-nav-item").elements().length)
			.toBe(2);
		const el = nav.element();
		expect(getComputedStyle(el).whiteSpace).toBe("nowrap");
		expect(el.scrollWidth).toBeGreaterThan(el.clientWidth);
		// Neither the page nor any scroller around the row scrolls sideways.
		const wide: string[] = [];
		for (let a = el.parentElement; a; a = a.parentElement)
			if (a.scrollWidth > a.clientWidth + 1)
				wide.push(`${a.tagName}.${a.className}`);
		expect(wide).toEqual([]);
	});

	test("a project page has no breadcrumb of its own, and the shared one leads back", async () => {
		await page.viewport(390, 844);
		setup(() => []);
		await renderApp("/agents/coder/projects/lsegal%2Fforgo");
		await expectSharedNavOnly();
		await expect.element(page.getByTestId("settings-back")).toBeInViewport();
		await backToBoard();
	});
});
