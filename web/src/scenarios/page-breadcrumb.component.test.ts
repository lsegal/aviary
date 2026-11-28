import { beforeEach, expect, test } from "vitest";
import { commands, type Locator, page } from "vitest/browser";
import { currentURL, renderApp } from "../component-test/app";
import { mockMCP } from "../component-test/server";
import { useSidebarLayout } from "../composables/useSidebarLayout";
import { expectSettingsHeader } from "./helpers/settingsHeader";

// Every nested page shows where it is, from its agent or System down, with a
// back link to its parent, also with the sidebar collapsed and on narrow
// screens; the collapsed rail lists each page's items in a flyout (#1036).
// Every one shows them in the same shared page header (#1064): the
// Scheduled Tasks task page's header from before #1067 (#1147).

const LONG_PROJECT = "lsegal/a-repository-with-a-very-long-name-indeed";

/** Mocks agent "Aviary" with channel "signal", a task and two projects. */
function mockAviary(sessions: object[] = []) {
	mockMCP({
		agent_list: [{ id: "Aviary", name: "Aviary", model: "x/y", state: "idle" }],
		config_get: {
			agents: [
				{
					name: "Aviary",
					model: "x/y",
					channels: [{ type: "signal", id: "signal", deliver_to: "+1" }],
					tasks: [{ name: "release-aviary", every: "1h", prompt: "release" }],
					projects: [{ target: "o/r" }, { target: LONG_PROJECT }],
				},
			],
		},
		config_validate: [],
		auth_list: [],
		tool_list: [],
		job_list: [],
		job_query: [],
		task_list: [],
		session_list: sessions,
		session_messages: [],
		channel_sessions: [],
		project_status: {
			states: { starting: 0, paused: 0, running: 0, done: 0, failed: 0 },
			targets: ["o/r", LONG_PROJECT],
		},
		project_job_list: { jobs: [], total: 0, offset: 0, limit: 50 },
	});
}

function breadcrumb() {
	return page.getByTestId("settings-breadcrumb");
}

/** Each matched element's whitespace-normalized text, as Playwright's
 * toHaveText with an array compares them. */
function texts(loc: Locator): string[] {
	return loc
		.elements()
		.map((el) => (el.textContent ?? "").replace(/\s+/g, " ").trim());
}

async function expectTexts(loc: Locator, expected: string[]) {
	await expect.poll(() => texts(loc)).toEqual(expected);
}

beforeEach(() => {
	mockAviary();
	// The collapsed sidebar is module state, read from localStorage once; a
	// spec's fresh page starts with it expanded.
	useSidebarLayout().collapsed.value = false;
});

// "the agent crumb opens the first page the user's sidebar shows" is in
// page-breadcrumb-nav-prefs: navigation preferences are fetched once per
// page load, so it needs a fresh page.

test("each nested page shows the shared header with its crumbs and parent link", async () => {
	const cases: Array<{
		url: string;
		crumbs: string[];
		back: string;
		parent: string;
	}> = [
		{
			url: "/agents/Aviary/channels/signal",
			crumbs: ["Aviary", "Channels", "signal"],
			back: "Channels",
			parent: "/agents/Aviary/channels",
		},
		{
			url: "/agents/Aviary/channels",
			crumbs: ["Aviary", "Channels"],
			back: "Aviary",
			parent: "/agents/Aviary/chat",
		},
		{
			url: "/agents/Aviary/projects/o%2Fr",
			crumbs: ["Aviary", "Code Loop", "o/r"],
			back: "Code Loop",
			parent: "/agents/Aviary/projects",
		},
		{
			url: "/agents/Aviary/projects/o%2Fr/jobs/1013",
			crumbs: ["Aviary", "Code Loop", "o/r", "#1013"],
			back: "o/r",
			parent: "/agents/Aviary/projects/o%2Fr",
		},
		{
			url: "/agents/Aviary/tasks/release-aviary",
			crumbs: ["Aviary", "Scheduled Tasks", "release-aviary"],
			back: "Scheduled Tasks",
			parent: "/agents/Aviary/tasks",
		},
		{
			url: "/agents/Aviary/tasks/release-aviary/runs/job_1",
			crumbs: ["Aviary", "Scheduled Tasks", "release-aviary", "run job_1"],
			back: "release-aviary",
			parent: "/agents/Aviary/tasks/release-aviary",
		},
		{
			url: "/agents/Aviary/tasks?task=release-aviary",
			crumbs: ["Aviary", "Scheduled Tasks", "release-aviary", "Settings"],
			back: "release-aviary",
			parent: "/agents/Aviary/tasks/release-aviary",
		},
		{
			url: "/agents/Aviary/tasks",
			crumbs: ["Aviary", "Scheduled Tasks"],
			back: "Aviary",
			parent: "/agents/Aviary/chat",
		},
		{
			url: "/agents/Aviary/channels/signal/settings",
			crumbs: ["Aviary", "Channels", "signal", "Settings"],
			back: "signal",
			parent: "/agents/Aviary/channels/signal",
		},
		{
			url: "/agents/Aviary/settings",
			crumbs: ["Aviary", "Agent Settings"],
			back: "Aviary",
			parent: "/agents/Aviary/chat",
		},
		{
			url: "/agents/Aviary/settings/permissions",
			crumbs: ["Aviary", "Agent Settings", "Permissions"],
			back: "Agent Settings",
			parent: "/agents/Aviary/settings",
		},
		{
			url: "/agents/Aviary/files",
			crumbs: ["Aviary", "Files"],
			back: "Aviary",
			parent: "/agents/Aviary/chat",
		},
		{
			url: "/projects/settings?agent=Aviary&project=o%2Fr",
			crumbs: ["Code Loop", "o/r", "Settings"],
			back: "o/r",
			parent: "/agents/Aviary/projects/o%2Fr",
		},
		{
			url: "/system/settings",
			crumbs: ["System", "Settings"],
			back: "System",
			parent: "/system/overview",
		},
		...[
			"Usage",
			"Tools",
			"Skills",
			"Sessions",
			"Models",
			"Logs",
			"Daemons",
		].map((label) => ({
			url: `/system/${label.toLowerCase()}`,
			crumbs: ["System", label],
			back: "System",
			parent: "/system/overview",
		})),
		{
			url: "/system/watches/w%2Fx/jobs/5",
			crumbs: ["System", "Watch: w/x", "#5"],
			back: "Watch: w/x",
			parent: "/system/watches/w%2Fx",
		},
	];
	for (const c of cases) {
		await renderApp(c.url);
		// One header, and no back link or breadcrumb of the page's own.
		const header = page.getByTestId("settings-header");
		await expect.poll(() => header.elements().length).toBe(1);
		await expect
			.poll(
				() =>
					page.getByRole("navigation", { name: "Breadcrumb" }).elements()
						.length,
			)
			.toBe(1);
		await expect
			.poll(
				() => page.getByRole("link", { name: /^Back to/ }).elements().length,
			)
			.toBe(1);
		const crumbs = header.getByTestId("settings-breadcrumb");
		await expect.element(crumbs).toHaveRole("navigation");
		await expect.element(crumbs).toHaveAccessibleName("Breadcrumb");
		await expectTexts(crumbs.getByRole("listitem"), c.crumbs);
		// Every ancestor links; the title may hold its own outside link.
		await expect
			.poll(() =>
				crumbs
					.getByRole("listitem")
					.elements()
					.filter((li) => !li.querySelector('[aria-current="page"]'))
					.flatMap((li) => [...li.querySelectorAll("a")])
					.map((a) => (a.textContent ?? "").replace(/\s+/g, " ").trim()),
			)
			.toEqual(c.crumbs.slice(0, -1));
		const current = crumbs.locator('[aria-current="page"]');
		await expect.element(current).toHaveText(c.crumbs.at(-1) as string);
		await expect.element(current).not.toHaveRole("link");
		const back = page.getByTestId("settings-back");
		await expect.element(back).toHaveAccessibleName(`Back to ${c.back}`);
		await expect.element(back).toHaveAttribute("href", c.parent);
		// A Code Loop board's header is the compact one (#1570).
		await expectSettingsHeader(true, c.url.endsWith("/projects/o%2Fr"));
	}
}, 60_000);

test("an agent page links no crumb back to itself", async () => {
	await renderApp("/agents/Aviary/chat");
	await expectTexts(breadcrumb().getByRole("listitem"), ["Aviary", "Chats"]);
	// The agent's crumb would open this same page, so nothing links.
	await expect
		.poll(() => breadcrumb().getByRole("link").elements().length)
		.toBe(0);
	await expectSettingsHeader(false);
	// The System overview is a top-level page with nothing above it.
	await renderApp("/system/overview");
	await expectTexts(breadcrumb().getByRole("listitem"), ["System", "Overview"]);
	await expectSettingsHeader(false);
});

test("settings pages show the same breadcrumb as their title, once", async () => {
	await renderApp("/agents/Aviary/tasks");
	const navs = page.getByRole("navigation", { name: "Breadcrumb" });
	await expect.poll(() => navs.elements().length).toBe(1);
	await expect
		.element(navs)
		.toHaveAttribute("data-testid", "settings-breadcrumb");
	await expectTexts(navs.getByRole("listitem"), ["Aviary", "Scheduled Tasks"]);
	await expect
		.element(page.getByTestId("settings-back"))
		.toHaveAccessibleName("Back to Aviary");
});

test("with the sidebar collapsed the breadcrumb and back link lead to the parent", async () => {
	await renderApp("/agents/Aviary/channels/signal");
	await page.getByTestId("sidebar-collapse").click();
	await expect.element(page.getByTestId("sidebar-rail")).toBeVisible();
	await expect.element(breadcrumb()).toBeVisible();
	await expect.element(breadcrumb()).toBeInViewport();

	await page.getByTestId("settings-back").click();
	await expect.poll(currentURL).toBe("/agents/Aviary/channels");
	await expectTexts(breadcrumb().getByRole("listitem"), ["Aviary", "Channels"]);
	await breadcrumb().getByRole("link", { name: "Aviary" }).click();
	await expect.poll(currentURL).toMatch(/\/agents\/Aviary\/chat/);
});

test("on a phone the breadcrumb collapses to the parent and the page (#1147)", async () => {
	await page.viewport(375, 740);
	await renderApp(
		`/agents/Aviary/projects/${encodeURIComponent(LONG_PROJECT)}/jobs/1013`,
	);
	const crumbs = breadcrumb();
	await expect.element(crumbs).toBeVisible();
	await expect.element(page.getByTestId("settings-back")).toBeVisible();
	await expectTexts(crumbs.getByRole("listitem"), [LONG_PROJECT, "#1013"]);
	await expect
		.element(page.getByTestId("settings-back"))
		.toHaveAccessibleName(`Back to ${LONG_PROJECT}`);
	const nav = document.querySelector('[data-testid="settings-breadcrumb"]');
	const items = [...(nav?.querySelectorAll("li") ?? [])].map((li) => {
		const text = li.querySelector("a, span:not([aria-hidden]), h2");
		return {
			truncated: !!text && text.scrollWidth > text.clientWidth,
			right: li.getBoundingClientRect().right,
		};
	});
	const pageOverflow = document.documentElement.scrollWidth - window.innerWidth;
	expect(pageOverflow).toBeLessThanOrEqual(0);
	for (const item of items)
		expect(item.right).toBeLessThanOrEqual(window.innerWidth);
	// The long parent gives way, while the current job keeps its name whole.
	const truncated = items.map((i) => i.truncated);
	expect(truncated[2]).toBe(true);
	expect(truncated[3]).toBe(false);
	// The title row's actions wrap below the title instead of scrolling.
	const titleRow = () => {
		const el = page.getByTestId("settings-title-row").element();
		return {
			overflow: el.scrollWidth - el.clientWidth,
			title:
				el
					.querySelector('[data-testid="settings-title"]')
					?.getBoundingClientRect().bottom ?? 0,
			actions:
				el
					.querySelector('[data-testid="settings-actions"]')
					?.getBoundingClientRect().top ?? 0,
		};
	};
	await expect.poll(() => titleRow().overflow).toBeLessThanOrEqual(0);
	await expect
		.poll(() => titleRow().actions - titleRow().title)
		.toBeGreaterThanOrEqual(0);
});

test("the rail marks the open chat without looking unread", async () => {
	// The chats from #1319's report: the open be-nice chat must not show a
	// dot on the Chats icon or a bold flyout entry, which read as unread.
	mockAviary([
		{ id: "sess_main", agent_id: "Aviary", name: "main", created_at: "" },
		{ id: "sess_benice", agent_id: "Aviary", name: "be-nice", created_at: "" },
	]);
	await renderApp("/agents/Aviary/chat/sess_benice");
	await page.getByTestId("sidebar-collapse").click();
	const chats = page
		.getByTestId("sidebar-rail")
		.getByTestId("sidebar-rail-chat");
	await expect.element(chats).toHaveAttribute("data-active-child", "true");
	await expect
		.poll(() => chats.getByTestId("sidebar-rail-dot").elements().length)
		.toBe(0);
	const marker = chats
		.getByTestId("sidebar-rail-active-child")
		.element()
		.getBoundingClientRect();
	expect(marker.width).toBeGreaterThan(marker.height * 2);

	await chats.hover();
	const flyout = page.getByTestId("sidebar-rail-flyout");
	const open = flyout.getByRole("link", { name: "be-nice" });
	await expect.element(open).toHaveAttribute("aria-current", "page");
	const weights = flyout
		.getByTestId("sidebar-rail-flyout-item")
		.elements()
		.map((el) => getComputedStyle(el).fontWeight);
	expect(weights).toEqual(["400", "400"]);
});

test("collapsed rail icons list their items in a flyout on hover and focus", async () => {
	await renderApp("/agents/Aviary/channels/signal");
	await page.getByTestId("sidebar-collapse").click();
	const rail = page.getByTestId("sidebar-rail");

	// The active Channels icon marks that one of its channels is open.
	const channels = rail.getByTestId("sidebar-rail-channels");
	await expect.element(channels).toHaveAttribute("data-active-child", "true");
	await expect
		.element(channels.getByTestId("sidebar-rail-active-child"))
		.toBeVisible();
	await expect
		.element(rail.getByTestId("sidebar-rail-projects"))
		.not.toHaveAttribute("data-active-child");

	await channels.hover();
	const flyout = page.getByTestId("sidebar-rail-flyout");
	await expect.element(flyout.getByRole("tooltip")).toHaveText("Channels");
	const signal = flyout.getByRole("link", { name: "signal" });
	await expect.element(signal).toHaveAttribute("aria-current", "page");

	// The pointer crosses onto the flyout, and an item opens its page
	// without expanding the sidebar or peeking it.
	await rail.getByTestId("sidebar-rail-projects").hover();
	await expect.element(flyout.getByRole("tooltip")).toHaveText("Code Loop");
	await flyout.getByRole("link", { name: "o/r" }).hover();
	await new Promise((r) => setTimeout(r, 500));
	await expect.element(page.getByTestId("sidebar")).not.toBeInTheDocument();
	await flyout.getByRole("link", { name: "o/r" }).click();
	await expect.poll(currentURL).toBe("/agents/Aviary/projects/o%2Fr");
	await expect.element(rail).toBeVisible();
	await expect.element(flyout).not.toBeInTheDocument();
	await expect
		.element(rail.getByTestId("sidebar-rail-projects"))
		.toHaveAttribute("data-active-child", "true");

	// By keyboard: focusing the icon opens its flyout, and Tab moves into it.
	(rail.getByTestId("sidebar-rail-tasks").element() as HTMLElement).focus();
	await expect
		.element(flyout.getByRole("tooltip"))
		.toHaveText("Scheduled Tasks");
	await commands.keyPress("Tab");
	const task = flyout.getByRole("link", { name: "release-aviary" });
	await expect.element(task).toHaveFocus();
	await commands.keyPress("Enter");
	await expect.poll(currentURL).toBe("/agents/Aviary/tasks/release-aviary");

	// System lists its pages.
	await rail.getByTestId("sidebar-rail-system").hover();
	await expectTexts(flyout.getByRole("link"), [
		"Usage",
		"Tools",
		"Skills",
		"Sessions",
		"Models",
		"Logs",
		"Daemons",
	]);
	await commands.keyPress("Escape");
});
