import { mockMCP, setAuthToken } from "./helpers/mockMCP";
import { expectSettingsHeader } from "./helpers/settingsHeader";
import { expect, type Page, test } from "./helpers/test";

// Every routed page shows the one shared page header (#1147): the Scheduled
// Tasks task page's header from before #1067, a breadcrumb bar with an arrow
// back to the parent, then a title row with the section's icon tile, the
// title and the page's actions. No page keeps a header of its own.

/** Mocks agent "Aviary" with a channel, a task, a file and a project. */
async function mockAviary(page: Page) {
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
					projects: [{ target: "o/r" }],
				},
			],
		},
		config_validate: [],
		auth_list: [],
		tool_list: [],
		job_list: [],
		job_query: [],
		task_list: [],
		session_list: [],
		session_messages: [],
		channel_sessions: [],
		agent_file_list: ["AGENTS.md"],
		agent_file_read: "Agent rules",
		project_status: {
			states: { starting: 0, paused: 0, running: 0, done: 0, failed: 0 },
			targets: ["o/r"],
		},
		project_job_list: { jobs: [], total: 0, offset: 0, limit: 50 },
	});
}

const A = "/agents/Aviary";

/** Every routed page, with its title and whether it has a parent. */
const PAGES: Array<{ url: string; title: string; back?: false }> = [
	{ url: "/system/overview", title: "Overview", back: false },
	{ url: "/system/usage", title: "Usage" },
	{ url: "/system/tools", title: "Tools" },
	{ url: "/system/skills", title: "Skills" },
	{ url: "/system/sessions", title: "Sessions" },
	{ url: "/system/models", title: "Models" },
	{ url: "/system/logs", title: "Logs" },
	{ url: "/system/daemons", title: "Daemons" },
	{ url: "/system/settings", title: "Settings" },
	{ url: "/system/providers", title: "Settings" },
	{ url: "/system/display", title: "Settings" },
	{ url: "/system/watches/w%2Fx", title: "Watch: w/x" },
	{ url: "/system/watches/w%2Fx/jobs/5", title: "#5" },
	{ url: "/projects/settings", title: "Settings" },
	{ url: `${A}/chat`, title: "Chats", back: false },
	{ url: `${A}/projects`, title: "Code Loop" },
	{ url: `${A}/projects/o%2Fr`, title: "o/r" },
	{ url: `${A}/projects/o%2Fr/jobs/1013`, title: "#1013" },
	{ url: `${A}/channels`, title: "Channels" },
	{ url: `${A}/channels/signal`, title: "signal" },
	{ url: `${A}/channels/signal/settings`, title: "Settings" },
	{ url: `${A}/tasks`, title: "Scheduled Tasks" },
	{ url: `${A}/tasks?task=release-aviary`, title: "Settings" },
	{ url: `${A}/tasks/release-aviary`, title: "release-aviary" },
	{ url: `${A}/tasks/release-aviary/runs/job_1`, title: "run job_1" },
	{ url: `${A}/files?file=AGENTS.md`, title: "AGENTS.md" },
	{ url: `${A}/settings`, title: "Agent Settings" },
	{ url: `${A}/settings/permissions`, title: "Permissions" },
];

// Test IDs of the headers pages drew before #1147.
const OLD_HEADERS = [
	"page-header",
	"page-breadcrumb",
	"task-header",
	"job-detail-header",
	"channels-overview-header",
];

test.beforeEach(async ({ page }) => {
	await mockAviary(page);
});

for (const p of PAGES) {
	test(`${p.url} renders the shared page header and no other`, async ({
		page,
	}) => {
		{
			await page.goto(p.url);
			const header = page.getByTestId("settings-header");
			await expect(page.getByTestId("settings-title")).toHaveText(p.title);
			await expectSettingsHeader(page, p.back !== false);
			// The header opens the page, and it is the page's only header.
			expect(
				await header.evaluate(
					(el) =>
						el.parentElement?.tagName === "MAIN" && !el.previousElementSibling,
				),
			).toBe(true);
			await expect(page.locator("main header:visible")).toHaveCount(0);
			await expect(page.locator("main h1:visible")).toHaveCount(0);
			await expect(
				page.getByRole("navigation", { name: "Breadcrumb" }),
			).toHaveCount(1);
			await expect(page.locator("main h2.text-xl:visible")).toHaveCount(0);
			for (const id of OLD_HEADERS)
				await expect(page.getByTestId(id)).toHaveCount(0);
			// The current crumb is not a link; every parent crumb is.
			const current = header.locator('[aria-current="page"]');
			await expect(current).toHaveCount(1);
			await expect(current.locator("a")).toHaveCount(0);
		}
	});

	test(`on a phone ${p.url}'s header collapses to its parent and page without scrolling sideways`, async ({
		page,
	}) => {
		await page.setViewportSize({ width: 375, height: 740 });
		{
			await page.goto(p.url);
			await expect(page.getByTestId("settings-title")).toHaveText(p.title);
			const crumbs = page
				.getByTestId("settings-breadcrumb")
				.getByRole("listitem");
			expect(await crumbs.count()).toBeLessThanOrEqual(2);
			await expect(crumbs.last().locator('[aria-current="page"]')).toHaveCount(
				1,
			);
			const overflow = await page
				.getByTestId("settings-header")
				.evaluate((el) =>
					[el, ...el.querySelectorAll("*")].some(
						(e) =>
							e.getBoundingClientRect().right > window.innerWidth + 1 &&
							getComputedStyle(e).display !== "none",
					),
				);
			expect(overflow).toBe(false);
		}
	});
}
