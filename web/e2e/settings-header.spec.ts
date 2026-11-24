import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";
import { expectSettingsHeader } from "./helpers/settingsHeader";

// Every Settings page shares one header: a back link to the page the
// settings belong to and a breadcrumb whose crumbs link to their pages (#986).
// It is the header every nested page shares (#1064). Every page keeps its
// look: sticky, bordered and blurred, below the page's padding (#1125).

const NOW = new Date().toISOString();

/** Mocks agent "Aviary" with channel "signal", task "release-aviary" and project o/r. */
async function mockSettings(page: Page) {
	const config = {
		agents: [
			{
				name: "Aviary",
				model: "x/y",
				channels: [{ type: "signal", id: "signal", deliver_to: "+1" }],
				tasks: [{ name: "release-aviary", every: "1h", prompt: "release" }],
				projects: [{ target: "o/r" }],
			},
		],
	};
	const saves: unknown[] = [];
	await setAuthToken(page);
	await mockMCP(page, {
		agent_list: [{ id: "Aviary", name: "Aviary", model: "x/y", state: "idle" }],
		config_get: () => config,
		config_save: (args) => {
			saves.push(args);
			Object.assign(config, JSON.parse(String(args?.config ?? "{}")));
			return "ok";
		},
		config_validate: [],
		auth_list: [],
		tool_list: [],
		job_list: [],
		job_query: [],
		session_list: [
			{ id: "main", agent_id: "Aviary", name: "main", created_at: NOW },
		],
		session_messages: [],
		channel_sessions: [
			{
				id: "channel:signal:signal",
				agent_id: "Aviary",
				name: "channel:signal:signal",
				created_at: NOW,
				own: true,
			},
		],
	});
	return { saves };
}

function breadcrumb(page: Page) {
	return page.getByRole("navigation", { name: "Breadcrumb" });
}

function backLink(page: Page) {
	return page.getByTestId("settings-back");
}

test("channel settings link back to the agent and the channel's conversation", async ({
	page,
}) => {
	await mockSettings(page);
	// Opened directly, as from a new tab: the back link still has a parent.
	await page.goto("/agents/Aviary/channels/signal/settings");
	const crumbs = breadcrumb(page);
	await expect(crumbs.getByRole("listitem")).toHaveText([
		"Aviary",
		"Channels",
		"signal",
		"Settings",
	]);
	const current = crumbs.locator('[aria-current="page"]');
	await expect(current).toHaveText("Settings");
	await expect(page.getByTestId("settings-title")).toHaveText("Settings");
	await expect(crumbs.getByRole("link", { name: "Settings" })).toHaveCount(0);
	// The separators are hidden from screen readers.
	await expect(crumbs).toMatchAriaSnapshot(`
		- navigation "Breadcrumb":
		  - list:
		    - listitem:
		      - link "Aviary"
		    - listitem:
		      - link "Channels"
		    - listitem:
		      - link "signal"
		    - listitem "Settings"
	`);
	await expect(backLink(page)).toHaveAccessibleName("Back to signal");

	await backLink(page).click();
	await expect(page).toHaveURL(/\/agents\/Aviary\/channels\/signal$/);
	await expect(page.getByTestId("settings-title")).toHaveText("signal");

	await page.goBack();
	await breadcrumb(page).getByRole("link", { name: "signal" }).click();
	await expect(page).toHaveURL(/\/agents\/Aviary\/channels\/signal$/);

	await page.goBack();
	await breadcrumb(page).getByRole("link", { name: "Aviary" }).click();
	await expect(page).toHaveURL(/\/agents\/Aviary\/chat/);
	await expect(page.getByTestId("unsaved-changes-dialog")).toHaveCount(0);
});

test("task, agent, project and System settings show crumbs to their own parents", async ({
	page,
}) => {
	await mockSettings(page);
	const cases: Array<{
		url: string;
		crumbs: string[];
		back: string;
		parent: RegExp;
	}> = [
		{
			url: "/agents/Aviary/tasks?task=release-aviary",
			crumbs: ["Aviary", "Scheduled Tasks", "release-aviary", "Settings"],
			back: "Back to release-aviary",
			parent: /\/agents\/Aviary\/tasks\/release-aviary$/,
		},
		{
			url: "/agents/Aviary/settings",
			crumbs: ["Aviary", "Agent Settings"],
			back: "Back to Aviary",
			parent: /\/agents\/Aviary\/chat/,
		},
		{
			url: "/agents/Aviary/settings/permissions",
			crumbs: ["Aviary", "Agent Settings", "Permissions"],
			back: "Back to Agent Settings",
			parent: /\/agents\/Aviary\/settings$/,
		},
		{
			url: "/projects/settings?agent=Aviary&project=o%2Fr",
			crumbs: ["Code Loop", "o/r", "Settings"],
			back: "Back to o/r",
			parent: /\/agents\/Aviary\/projects\/o%2Fr$/,
		},
		{
			url: "/projects/settings",
			crumbs: ["Code Loop", "Settings"],
			back: "Back to Code Loop",
			parent: /\/agents\/Aviary\/projects$/,
		},
		{
			url: "/system/settings",
			crumbs: ["System", "Settings"],
			back: "Back to System",
			parent: /\/system\/overview$/,
		},
		{
			url: "/system/sessions",
			crumbs: ["System", "Sessions"],
			back: "Back to System",
			parent: /\/system\/overview$/,
		},
	];
	for (const c of cases) {
		await page.goto(c.url);
		await expect(breadcrumb(page).getByRole("listitem")).toHaveText(c.crumbs);
		await expect(breadcrumb(page).locator('[aria-current="page"]')).toHaveText(
			c.crumbs.at(-1) as string,
		);
		await expect(backLink(page)).toHaveAccessibleName(c.back);
		await expectSettingsHeader(page);
		await backLink(page).click();
		await expect(page).toHaveURL(c.parent);
	}
});

test("settings pages' agent crumb opens the first page the user's sidebar shows", async ({
	page,
}) => {
	await mockSettings(page);
	await page.route("/api/preferences/navigation", (route) =>
		route.fulfill({
			status: 200,
			contentType: "application/json",
			body: JSON.stringify({
				order: { agentPages: ["chat", "projects", "channels"] },
				hidden: { agentPages: ["chat"] },
			}),
		}),
	);
	// Chats is hidden, so the agent's first page is Code Loop, as on the
	// task page (#1091).
	const home = "/agents/Aviary/projects";
	for (const url of [
		"/agents/Aviary/tasks/release-aviary",
		"/agents/Aviary/tasks?task=release-aviary",
		"/agents/Aviary/tasks",
		"/agents/Aviary/channels/signal/settings",
		"/agents/Aviary/settings/permissions",
		"/agents/Aviary/settings",
	]) {
		await page.goto(url);
		await expect(
			breadcrumb(page).getByRole("link", { name: "Aviary", exact: true }),
		).toHaveAttribute("href", home);
	}
	await expect(backLink(page)).toHaveAccessibleName("Back to Aviary");
	await backLink(page).click();
	await expect(page).toHaveURL(home);
});

test("the keyboard reaches the back link and each crumb in order", async ({
	page,
}) => {
	await mockSettings(page);
	await page.goto("/agents/Aviary/channels/signal/settings");
	const back = backLink(page);
	await expect(back).toBeVisible();
	await back.focus();
	await page.keyboard.press("Tab");
	await expect(
		breadcrumb(page).getByRole("link", { name: "Aviary" }),
	).toBeFocused();
	await page.keyboard.press("Tab");
	await expect(
		breadcrumb(page).getByRole("link", { name: "Channels" }),
	).toBeFocused();
	await page.keyboard.press("Tab");
	const channel = breadcrumb(page).getByRole("link", { name: "signal" });
	await expect(channel).toBeFocused();
	await page.keyboard.press("Enter");
	await expect(page).toHaveURL(/\/agents\/Aviary\/channels\/signal$/);
});

test("leaving through the header with unsaved edits asks first", async ({
	page,
}) => {
	await mockSettings(page);
	await page.goto("/agents/Aviary/channels/signal/settings");
	const deliverTo = page.getByTestId("channel-deliver-to");
	await expect(deliverTo).toHaveValue("+1");
	await deliverTo.fill("+2");

	const dialog = page.getByTestId("unsaved-changes-dialog");
	await backLink(page).click();
	await expect(dialog).toBeVisible();
	await dialog.getByRole("button", { name: "Stay" }).click();
	await expect(page).toHaveURL(/\/channels\/signal\/settings$/);
	await expect(deliverTo).toHaveValue("+2");

	await breadcrumb(page).getByRole("link", { name: "Aviary" }).click();
	await expect(dialog).toBeVisible();
	await dialog.getByRole("button", { name: "Stay" }).click();
	await expect(deliverTo).toHaveValue("+2");

	await breadcrumb(page).getByRole("link", { name: "signal" }).click();
	await dialog.getByRole("button", { name: "Leave" }).click();
	await expect(page).toHaveURL(/\/agents\/Aviary\/channels\/signal$/);

	// The edit was dropped: settings open on the saved value, and leaving
	// them untouched does not ask.
	await page.goto("/agents/Aviary/channels/signal/settings");
	await expect(page.getByTestId("channel-deliver-to")).toHaveValue("+1");
	await backLink(page).click();
	await expect(page).toHaveURL(/\/agents\/Aviary\/channels\/signal$/);
	await expect(dialog).toHaveCount(0);
});

test("the header fits a phone without horizontal overflow", async ({
	browser,
}) => {
	const context = await browser.newContext({
		viewport: { width: 375, height: 740 },
		hasTouch: true,
		isMobile: true,
		ignoreHTTPSErrors: true,
	});
	const page = await context.newPage();
	await mockSettings(page);
	for (const url of [
		"/agents/Aviary/channels/signal/settings",
		"/agents/Aviary/tasks?task=release-aviary",
		"/system/settings",
	]) {
		await page.goto(url);
		const header = page.getByTestId("settings-header");
		await expect(breadcrumb(page)).toBeVisible();
		await expect(backLink(page)).toBeInViewport();
		const overflow = await header.evaluate(
			(el) => el.scrollWidth - el.clientWidth,
		);
		expect(overflow).toBeLessThanOrEqual(0);
		const box = await header.boundingBox();
		expect((box?.x ?? 0) + (box?.width ?? 0)).toBeLessThanOrEqual(375);
		for (const link of await breadcrumb(page).getByRole("link").all())
			await expect(link).toBeInViewport();
	}
	await backLink(page).tap();
	await expect(page).toHaveURL(/\/system\/overview$/);
	await context.close();
});
