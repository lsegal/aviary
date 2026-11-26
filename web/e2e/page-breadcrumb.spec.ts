import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";
import { expectSettingsHeader } from "./helpers/settingsHeader";

// Every nested page shows where it is, from its agent or System down, with a
// back link to its parent, also with the sidebar collapsed and on narrow
// screens; the collapsed rail lists each page's items in a flyout (#1036).
// Every one shows them in the same shared page header (#1064): the
// Scheduled Tasks task page's header from before #1067 (#1147).

const LONG_PROJECT = "lsegal/a-repository-with-a-very-long-name-indeed";

/** Mocks agent "Aviary" with channel "signal", a task and two projects. */
async function mockAviary(page: Page, sessions: unknown[] = []) {
	await setAuthToken(page);
	await mockMCP(page, {
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

function breadcrumb(page: Page) {
	return page.getByTestId("settings-breadcrumb");
}

test.beforeEach(async ({ page }) => {
	await mockAviary(page);
});

test("each nested page shows the shared header with its crumbs and parent link", async ({
	page,
}) => {
	// It walks every nested page.
	test.slow();
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
		await page.goto(c.url);
		// One header, and no back link or breadcrumb of the page's own.
		const header = page.getByTestId("settings-header");
		await expect(header).toHaveCount(1);
		await expect(
			page.getByRole("navigation", { name: "Breadcrumb" }),
		).toHaveCount(1);
		await expect(page.getByRole("link", { name: /^Back to/ })).toHaveCount(1);
		const crumbs = header.getByTestId("settings-breadcrumb");
		await expect(crumbs).toHaveRole("navigation");
		await expect(crumbs).toHaveAccessibleName("Breadcrumb");
		await expect(crumbs.getByRole("listitem")).toHaveText(c.crumbs);
		// Every ancestor links; the title may hold its own outside link.
		await expect(
			crumbs
				.getByRole("listitem")
				.filter({ hasNot: page.locator('[aria-current="page"]') })
				.getByRole("link"),
		).toHaveText(c.crumbs.slice(0, -1));
		const current = crumbs.locator('[aria-current="page"]');
		await expect(current).toHaveText(c.crumbs.at(-1) as string);
		await expect(current).not.toHaveRole("link");
		const back = page.getByTestId("settings-back");
		await expect(back).toHaveAccessibleName(`Back to ${c.back}`);
		await expect(back).toHaveAttribute("href", c.parent);
		await expectSettingsHeader(page);
	}
});

test("an agent page links no crumb back to itself", async ({ page }) => {
	await page.goto("/agents/Aviary/chat");
	await expect(breadcrumb(page).getByRole("listitem")).toHaveText([
		"Aviary",
		"Chats",
	]);
	// The agent's crumb would open this same page, so nothing links.
	await expect(breadcrumb(page).getByRole("link")).toHaveCount(0);
	await expectSettingsHeader(page, false);
	// The System overview is a top-level page with nothing above it.
	await page.goto("/system/overview");
	await expect(breadcrumb(page).getByRole("listitem")).toHaveText([
		"System",
		"Overview",
	]);
	await expectSettingsHeader(page, false);
});

test("settings pages show the same breadcrumb as their title, once", async ({
	page,
}) => {
	await page.goto("/agents/Aviary/tasks");
	const navs = page.getByRole("navigation", { name: "Breadcrumb" });
	await expect(navs).toHaveCount(1);
	await expect(navs).toHaveAttribute("data-testid", "settings-breadcrumb");
	await expect(navs.getByRole("listitem")).toHaveText([
		"Aviary",
		"Scheduled Tasks",
	]);
	await expect(page.getByTestId("settings-back")).toHaveAccessibleName(
		"Back to Aviary",
	);
});

test("with the sidebar collapsed the breadcrumb and back link lead to the parent", async ({
	page,
}) => {
	await page.goto("/agents/Aviary/channels/signal");
	await page.getByTestId("sidebar-collapse").click();
	await expect(page.getByTestId("sidebar-rail")).toBeVisible();
	await expect(breadcrumb(page)).toBeVisible();
	await expect(breadcrumb(page)).toBeInViewport();

	await page.getByTestId("settings-back").click();
	await expect(page).toHaveURL("/agents/Aviary/channels");
	await expect(breadcrumb(page).getByRole("listitem")).toHaveText([
		"Aviary",
		"Channels",
	]);
	await breadcrumb(page).getByRole("link", { name: "Aviary" }).click();
	await expect(page).toHaveURL(/\/agents\/Aviary\/chat/);
});

test("the agent crumb opens the first page the user's sidebar shows", async ({
	page,
}) => {
	await page.route("/api/preferences/navigation", (route) =>
		route.fulfill({
			status: 200,
			contentType: "application/json",
			body: JSON.stringify({
				order: {
					agentPages: [
						"tasks",
						"chat",
						"projects",
						"channels",
						"files",
						"settings",
					],
				},
				hidden: { agentPages: ["tasks"] },
			}),
		}),
	);
	await page.goto("/agents/Aviary/channels/signal");
	// Scheduled Tasks comes first but is hidden, so Chats is the first shown.
	await expect(
		breadcrumb(page).getByRole("link", { name: "Aviary" }),
	).toHaveAttribute("href", "/agents/Aviary/chat");
	await page.route("/api/preferences/navigation", (route) =>
		route.fulfill({
			status: 200,
			contentType: "application/json",
			body: JSON.stringify({
				order: { agentPages: ["projects", "chat"] },
				hidden: {},
			}),
		}),
	);
	await page.reload();
	await expect(
		breadcrumb(page).getByRole("link", { name: "Aviary" }),
	).toHaveAttribute("href", "/agents/Aviary/projects");
	// Settings pages' agent crumb and back link follow the same order (#1091).
	for (const url of ["/agents/Aviary/settings", "/agents/Aviary/tasks"]) {
		await page.goto(url);
		await expect(
			breadcrumb(page).getByRole("link", { name: "Aviary" }),
		).toHaveAttribute("href", "/agents/Aviary/projects");
		await expect(page.getByTestId("settings-back")).toHaveAttribute(
			"href",
			"/agents/Aviary/projects",
		);
	}
});

test("on a phone the breadcrumb collapses to the parent and the page (#1147)", async ({
	page,
}) => {
	await page.setViewportSize({ width: 375, height: 740 });
	await page.goto(
		`/agents/Aviary/projects/${encodeURIComponent(LONG_PROJECT)}/jobs/1013`,
	);
	const crumbs = breadcrumb(page);
	await expect(crumbs).toBeVisible();
	await expect(page.getByTestId("settings-back")).toBeVisible();
	await expect(crumbs.getByRole("listitem")).toHaveText([
		LONG_PROJECT,
		"#1013",
	]);
	await expect(page.getByTestId("settings-back")).toHaveAccessibleName(
		`Back to ${LONG_PROJECT}`,
	);
	const sizes = await page.evaluate(() => {
		const nav = document.querySelector('[data-testid="settings-breadcrumb"]');
		const items = [...(nav?.querySelectorAll("li") ?? [])].map((li) => {
			const text = li.querySelector("a, span:not([aria-hidden]), h2");
			return {
				truncated: !!text && text.scrollWidth > text.clientWidth,
				right: li.getBoundingClientRect().right,
			};
		});
		return {
			items,
			pageOverflow: document.documentElement.scrollWidth - window.innerWidth,
			innerWidth: window.innerWidth,
		};
	});
	expect(sizes.pageOverflow).toBeLessThanOrEqual(0);
	for (const item of sizes.items)
		expect(item.right).toBeLessThanOrEqual(sizes.innerWidth);
	// The long parent gives way, while the current job keeps its name whole.
	const truncated = sizes.items.map((i) => i.truncated);
	expect(truncated[2]).toBe(true);
	expect(truncated[3]).toBe(false);
	// The title row's actions wrap below the title instead of scrolling.
	const row = await page.getByTestId("settings-title-row").evaluate((el) => ({
		overflow: el.scrollWidth - el.clientWidth,
		title: el
			.querySelector('[data-testid="settings-title"]')
			?.getBoundingClientRect().bottom,
		actions: el
			.querySelector('[data-testid="settings-actions"]')
			?.getBoundingClientRect().top,
	}));
	expect(row.overflow).toBeLessThanOrEqual(0);
	expect(row.actions ?? 0).toBeGreaterThanOrEqual(row.title ?? 0);
});

test("the rail marks the open chat without looking unread", async ({
	page,
}) => {
	// The chats from #1319's report: the open be-nice chat must not show a
	// dot on the Chats icon or a bold flyout entry, which read as unread.
	await mockAviary(page, [
		{ id: "sess_main", agent_id: "Aviary", name: "main", created_at: "" },
		{ id: "be-nice", agent_id: "Aviary", name: "be-nice", created_at: "" },
	]);
	await page.goto("/agents/Aviary/chat/be-nice");
	await page.getByTestId("sidebar-collapse").click();
	const chats = page.getByTestId("sidebar-rail").getByTestId("sidebar-rail-chat");
	await expect(chats).toHaveAttribute("data-active-child", "true");
	await expect(chats.getByTestId("sidebar-rail-dot")).toHaveCount(0);
	const marker = await chats
		.getByTestId("sidebar-rail-active-child")
		.boundingBox();
	expect(marker?.width).toBeGreaterThan((marker?.height ?? 0) * 2);

	await chats.hover();
	const flyout = page.getByTestId("sidebar-rail-flyout");
	const open = flyout.getByRole("link", { name: "be-nice" });
	await expect(open).toHaveAttribute("aria-current", "page");
	const weights = await flyout
		.getByTestId("sidebar-rail-flyout-item")
		.evaluateAll((els) => els.map((el) => getComputedStyle(el).fontWeight));
	expect(weights).toEqual(["400", "400"]);
});

test("collapsed rail icons list their items in a flyout on hover and focus", async ({
	page,
}) => {
	await page.goto("/agents/Aviary/channels/signal");
	await page.getByTestId("sidebar-collapse").click();
	const rail = page.getByTestId("sidebar-rail");

	// The active Channels icon marks that one of its channels is open.
	const channels = rail.getByTestId("sidebar-rail-channels");
	await expect(channels).toHaveAttribute("data-active-child", "true");
	await expect(channels.getByTestId("sidebar-rail-active-child")).toBeVisible();
	await expect(rail.getByTestId("sidebar-rail-projects")).not.toHaveAttribute(
		"data-active-child",
	);

	await channels.hover();
	const flyout = page.getByTestId("sidebar-rail-flyout");
	await expect(flyout.getByRole("tooltip")).toHaveText("Channels");
	const signal = flyout.getByRole("link", { name: "signal" });
	await expect(signal).toHaveAttribute("aria-current", "page");

	// The pointer crosses onto the flyout, and an item opens its page
	// without expanding the sidebar or peeking it.
	await rail.getByTestId("sidebar-rail-projects").hover();
	await expect(flyout.getByRole("tooltip")).toHaveText("Code Loop");
	await flyout.getByRole("link", { name: "o/r" }).hover();
	await page.waitForTimeout(500);
	await expect(page.getByTestId("sidebar")).toHaveCount(0);
	await flyout.getByRole("link", { name: "o/r" }).click();
	await expect(page).toHaveURL("/agents/Aviary/projects/o%2Fr");
	await expect(rail).toBeVisible();
	await expect(flyout).toHaveCount(0);
	await expect(rail.getByTestId("sidebar-rail-projects")).toHaveAttribute(
		"data-active-child",
		"true",
	);

	// By keyboard: focusing the icon opens its flyout, and Tab moves into it.
	await rail.getByTestId("sidebar-rail-tasks").focus();
	await expect(flyout.getByRole("tooltip")).toHaveText("Scheduled Tasks");
	await page.keyboard.press("Tab");
	const task = flyout.getByRole("link", { name: "release-aviary" });
	await expect(task).toBeFocused();
	await page.keyboard.press("Enter");
	await expect(page).toHaveURL("/agents/Aviary/tasks/release-aviary");

	// System lists its pages.
	await rail.getByTestId("sidebar-rail-system").hover();
	await expect(flyout.getByRole("link")).toHaveText([
		"Usage",
		"Tools",
		"Skills",
		"Sessions",
		"Models",
		"Logs",
		"Daemons",
	]);
	await page.keyboard.press("Escape");
});
