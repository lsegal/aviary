import { expect, test } from "vitest";
import { commands, type Locator, page } from "vitest/browser";
import { currentURL, goBack, goto, renderApp } from "../component-test/app";
import { declarePages } from "../component-test/routes";
import { mockMCP } from "../component-test/server";
import { expectSettingsHeader } from "./helpers/settingsHeader";

// The pages this file renders (see component-test/routes.ts).
declarePages(
	() => import("../views/ChannelView.vue"),
	() => import("../views/ChatView.vue"),
	() => import("../views/OverviewView.vue"),
	() => import("../views/ProjectsView.vue"),
	() => import("../views/SettingsView.vue"),
	() => import("../views/TaskView.vue"),
);

// Every Settings page shares one header: a back link to the page the
// settings belong to and a breadcrumb whose crumbs link to their pages (#986).
// It is the header every nested page shares (#1064). Every page keeps its
// look: sticky, bordered and blurred, below the page's padding (#1125).
//
// "settings pages' agent crumb opens the first page the user's sidebar
// shows" is in settings-header-nav-prefs: navigation preferences are
// fetched once per page load (module state), so it needs a fresh page.

const NOW = new Date().toISOString();

/** Mocks agent "Aviary" with channel "signal", task "release-aviary" and project o/r. */
function mockSettings() {
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
	mockMCP({
		agent_list: [{ id: "Aviary", name: "Aviary", model: "x/y", state: "idle" }],
		config_get: () => config,
		config_save: (args?: Record<string, unknown>) => {
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

function breadcrumb() {
	return page.getByRole("navigation", { name: "Breadcrumb" });
}

function backLink() {
	return page.getByTestId("settings-back");
}

function count(loc: Locator): number {
	return loc.elements().length;
}

/** Each matched element's whitespace-normalized text, as Playwright's
 * toHaveText with an array compares them. */
function texts(loc: Locator): string[] {
	return loc
		.elements()
		.map((el) => (el.textContent ?? "").replace(/\s+/g, " ").trim());
}

/** An element's text as screen readers get it: without aria-hidden parts. */
function ariaText(el: Element): string {
	let out = "";
	for (const node of el.childNodes) {
		if (node.nodeType === Node.TEXT_NODE) out += node.textContent;
		else if (
			node instanceof Element &&
			node.getAttribute("aria-hidden") !== "true"
		)
			out += ` ${ariaText(node)} `;
	}
	return out.replace(/\s+/g, " ").trim();
}

test("channel settings link back to the agent and the channel's conversation", async () => {
	mockSettings();
	// Opened directly, as from a new tab: the back link still has a parent.
	await renderApp("/agents/Aviary/channels/signal/settings");
	const crumbs = breadcrumb();
	await expect
		.poll(() => texts(crumbs.getByRole("listitem")))
		.toEqual(["Aviary", "Channels", "signal", "Settings"]);
	const current = crumbs.locator('[aria-current="page"]');
	await expect.element(current).toHaveText("Settings");
	await expect
		.element(page.getByTestId("settings-title"))
		.toHaveText("Settings");
	expect(count(crumbs.getByRole("link", { name: "Settings" }))).toBe(0);
	// The separators are hidden from screen readers (the spec's aria
	// snapshot): one list of four items, the first three a link each, the
	// last the plain current page.
	const nav = crumbs.element();
	const lists = nav.querySelectorAll("ol, ul, [role=list]");
	expect(lists.length).toBe(1);
	const items = crumbs.getByRole("listitem").elements();
	expect(
		items.map((li) => ({
			text: ariaText(li),
			links: [...li.querySelectorAll("a")].map((a) => ariaText(a)),
		})),
	).toEqual([
		{ text: "Aviary", links: ["Aviary"] },
		{ text: "Channels", links: ["Channels"] },
		{ text: "signal", links: ["signal"] },
		{ text: "Settings", links: [] },
	]);
	await expect.element(backLink()).toHaveAccessibleName("Back to signal");

	await backLink().click();
	await expect.poll(currentURL).toMatch(/\/agents\/Aviary\/channels\/signal$/);
	await expect.element(page.getByTestId("settings-title")).toHaveText("signal");

	await goBack();
	await breadcrumb().getByRole("link", { name: "signal" }).click();
	await expect.poll(currentURL).toMatch(/\/agents\/Aviary\/channels\/signal$/);

	await goBack();
	await breadcrumb().getByRole("link", { name: "Aviary" }).click();
	await expect.poll(currentURL).toMatch(/\/agents\/Aviary\/chat/);
	expect(count(page.getByTestId("unsaved-changes-dialog"))).toBe(0);
});

test("task, agent, project and System settings show crumbs to their own parents", async () => {
	mockSettings();
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
		// A fresh page per URL, as page.goto loads one.
		await renderApp(c.url);
		await expect
			.poll(() => texts(breadcrumb().getByRole("listitem")))
			.toEqual(c.crumbs);
		await expect
			.element(breadcrumb().locator('[aria-current="page"]'))
			.toHaveText(c.crumbs.at(-1) as string);
		await expect.element(backLink()).toHaveAccessibleName(c.back);
		await expectSettingsHeader();
		await backLink().click();
		await expect.poll(currentURL).toMatch(c.parent);
	}
});

test("the keyboard reaches the back link and each crumb in order", async () => {
	mockSettings();
	await renderApp("/agents/Aviary/channels/signal/settings");
	const back = backLink();
	await expect.element(back).toBeVisible();
	(back.element() as HTMLElement).focus();
	await commands.keyPress("Tab");
	await expect
		.element(breadcrumb().getByRole("link", { name: "Aviary" }))
		.toHaveFocus();
	await commands.keyPress("Tab");
	await expect
		.element(breadcrumb().getByRole("link", { name: "Channels" }))
		.toHaveFocus();
	await commands.keyPress("Tab");
	const channel = breadcrumb().getByRole("link", { name: "signal" });
	await expect.element(channel).toHaveFocus();
	await commands.keyPress("Enter");
	await expect.poll(currentURL).toMatch(/\/agents\/Aviary\/channels\/signal$/);
});

test("leaving through the header with unsaved edits asks first", async () => {
	mockSettings();
	await renderApp("/agents/Aviary/channels/signal/settings");
	const deliverTo = page.getByTestId("channel-deliver-to");
	await expect.element(deliverTo).toHaveValue("+1");
	await deliverTo.fill("+2");

	const dialog = page.getByTestId("unsaved-changes-dialog");
	await backLink().click();
	await expect.element(dialog).toBeVisible();
	await dialog.getByRole("button", { name: "Stay" }).click();
	await expect.poll(currentURL).toMatch(/\/channels\/signal\/settings$/);
	await expect.element(deliverTo).toHaveValue("+2");

	await breadcrumb().getByRole("link", { name: "Aviary" }).click();
	await expect.element(dialog).toBeVisible();
	await dialog.getByRole("button", { name: "Stay" }).click();
	await expect.element(deliverTo).toHaveValue("+2");

	await breadcrumb().getByRole("link", { name: "signal" }).click();
	await dialog.getByRole("button", { name: "Leave" }).click();
	await expect.poll(currentURL).toMatch(/\/agents\/Aviary\/channels\/signal$/);

	// The edit was dropped: settings open on the saved value, and leaving
	// them untouched does not ask.
	await renderApp("/agents/Aviary/channels/signal/settings");
	await expect
		.element(page.getByTestId("channel-deliver-to"))
		.toHaveValue("+1");
	await backLink().click();
	await expect.poll(currentURL).toMatch(/\/agents\/Aviary\/channels\/signal$/);
	expect(count(dialog)).toBe(0);
});

test("System Sessions shows no config Reload or Save Changes (#1671)", async () => {
	mockSettings();
	// Nothing on Sessions is config, and Refresh Sessions reloads the table.
	await renderApp("/system/sessions");
	await expect
		.element(page.getByRole("button", { name: "Refresh Sessions" }))
		.toBeVisible();
	await expect
		.poll(
			() =>
				page.getByRole("button", { name: "Save Changes" }).elements().length,
		)
		.toBe(0);
	await expect
		.poll(() => page.getByRole("button", { name: "Reload" }).elements().length)
		.toBe(0);
	// System Settings still edits config, so it keeps both.
	await goto("/system/settings");
	await expect
		.element(page.getByRole("button", { name: "Save Changes" }))
		.toBeVisible();
	await expect
		.element(page.getByRole("button", { name: "Reload" }))
		.toBeVisible();
});

test("the header fits a phone without horizontal overflow", async () => {
	// The spec's touch context only changes select heights here
	// (pointer:coarse), never widths, so a phone-sized viewport covers it.
	await page.viewport(375, 740);
	mockSettings();
	for (const url of [
		"/agents/Aviary/channels/signal/settings",
		"/agents/Aviary/tasks?task=release-aviary",
		"/system/settings",
	]) {
		await renderApp(url);
		const header = page.getByTestId("settings-header");
		await expect.element(breadcrumb()).toBeVisible();
		await expect.element(backLink()).toBeInViewport();
		const el = header.element();
		expect(el.scrollWidth - el.clientWidth).toBeLessThanOrEqual(0);
		const box = el.getBoundingClientRect();
		expect(box.x + box.width).toBeLessThanOrEqual(375);
		for (const link of breadcrumb().getByRole("link").all())
			await expect.element(link).toBeInViewport();
	}
	await backLink().click();
	await expect.poll(currentURL).toMatch(/\/system\/overview$/);
});
