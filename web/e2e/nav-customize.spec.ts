import { mockMCP, setAuthToken } from "./helpers/mockMCP";
import { expect, type Page, test } from "./helpers/test";

// Customizing the navigation (#1003): the sidebar editor, in Settings →
// Display (#1093), reorders and hides pages, the result persists on the
// server with a localStorage fallback, and Reset restores the built-in
// order.

const DEFAULT_PAGES = [
	"chat",
	"projects",
	"channels",
	"tasks",
	"files",
	"settings",
];

async function mockAgents(page: Page, names: string[]) {
	await setAuthToken(page);
	await mockMCP(page, {
		agent_list: [],
		job_list: [],
		config_validate: [],
		session_list: [],
		config_get: {
			agents: names.map((name) => ({
				name,
				model: "anthropic/claude-sonnet-4-5",
				fallbacks: [],
			})),
		},
	});
}

/** The sole agent's sidebar page rows, by key, in order. */
async function sidebarPages(page: Page): Promise<string[]> {
	const rows = page
		.getByTestId("sidebar-sole-agent")
		.locator('[data-testid^="sidebar-row-"]');
	await expect(rows.first()).toBeVisible();
	return (await rows.evaluateAll((els) =>
		els.map((el) =>
			el.getAttribute("data-testid")?.replace("sidebar-row-", ""),
		),
	)) as string[];
}

async function systemPages(page: Page): Promise<string[]> {
	const links = page
		.getByTestId("sidebar-system-list")
		.locator('[data-testid^="sidebar-system-"]');
	await expect(links.first()).toBeVisible();
	return (await links.evaluateAll((els) =>
		els.map((el) =>
			el.getAttribute("data-testid")?.replace("sidebar-system-", ""),
		),
	)) as string[];
}

async function openEditor(page: Page) {
	await page.getByTestId("sidebar").getByTestId("gear-menu-button").click();
	await page.getByTestId("gear-menu-edit-nav").click();
	await expect(page).toHaveURL("/system/display#sidebar");
	await expect(page.getByTestId("nav-edit-agentPages")).toBeVisible();
}

function editRow(page: Page, list: string, key: string) {
	return page.getByTestId(`nav-edit-${list}`).locator(`[data-key="${key}"]`);
}

test.use({ viewport: { width: 1280, height: 900 } });

test("pages reorder by keyboard and hide, and persist on the server", async ({
	page,
}) => {
	await mockAgents(page, ["assistant"]);
	await page.goto("/system/overview");
	expect(await sidebarPages(page)).toEqual(DEFAULT_PAGES);

	await openEditor(page);
	const up = editRow(page, "agentPages", "tasks").getByTestId("nav-edit-up");
	await up.focus();
	await page.keyboard.press("Enter");
	// Focus follows the moved row, so the key repeats.
	await expect(up).toBeFocused();
	await page.keyboard.press("Enter");
	await editRow(page, "agentPages", "files")
		.getByTestId("nav-edit-visibility")
		.click();
	await expect(editRow(page, "agentPages", "files")).toHaveAttribute(
		"data-hidden",
		"true",
	);

	const arranged = ["chat", "tasks", "projects", "channels", "settings"];
	expect(await sidebarPages(page)).toEqual(arranged);

	// A fresh browser on another device reads the server's copy.
	await page.evaluate(() => localStorage.removeItem("aviary.nav.prefs"));
	await page.reload();
	expect(await sidebarPages(page)).toEqual(arranged);

	// The hidden page stays reachable through Show hidden.
	const toggle = page.getByTestId("sidebar-show-hidden");
	await expect(toggle).toHaveText("Show 1 hidden page");
	await toggle.click();
	expect(await sidebarPages(page)).toEqual([
		...arranged.slice(0, 4),
		"files",
		"settings",
	]);
	await toggle.click();
	expect(await sidebarPages(page)).toEqual(arranged);
});

test("System pages reorder by dragging their handle", async ({ page }) => {
	await mockAgents(page, ["assistant"]);
	await page.goto("/system/usage");
	expect((await systemPages(page)).slice(0, 3)).toEqual([
		"usage",
		"tools",
		"skills",
	]);

	await openEditor(page);
	const handle = editRow(page, "systemPages", "skills").getByTestId(
		"nav-edit-handle",
	);
	const target = await editRow(page, "systemPages", "usage").boundingBox();
	await handle.hover();
	await page.mouse.down();
	await page.mouse.move((target?.x ?? 0) + 20, (target?.y ?? 0) + 4, {
		steps: 8,
	});
	await page.mouse.up();
	await expect
		.poll(async () => (await rowKeys(page, "systemPages")).slice(0, 3))
		.toEqual(["skills", "usage", "tools"]);

	// The sidebar lists System pages on System pages.
	await page.goto("/system/usage");
	expect((await systemPages(page)).slice(0, 3)).toEqual([
		"skills",
		"usage",
		"tools",
	]);
	await page.reload();
	expect((await systemPages(page)).slice(0, 3)).toEqual([
		"skills",
		"usage",
		"tools",
	]);
});

test("Reset to default restores the order on the server and locally", async ({
	page,
}) => {
	await mockAgents(page, ["assistant"]);
	const deletes: string[] = [];
	page.on("request", (req) => {
		if (
			req.url().endsWith("/api/preferences/navigation") &&
			req.method() === "DELETE"
		)
			deletes.push(req.method());
	});
	await page.goto("/system/overview");
	await openEditor(page);
	const reset = page.getByTestId("nav-edit-reset");
	await expect(reset).toBeDisabled();
	await editRow(page, "agentPages", "settings")
		.getByTestId("nav-edit-up")
		.click();
	await editRow(page, "agentPages", "chat")
		.getByTestId("nav-edit-visibility")
		.click();
	await expect(reset).toBeEnabled();

	await reset.click();
	await expect(reset).toBeDisabled();
	expect(await sidebarPages(page)).toEqual(DEFAULT_PAGES);
	await expect(page.getByTestId("sidebar-show-hidden")).toHaveCount(0);
	expect(deletes).toHaveLength(1);
	expect(
		await page.evaluate(() => localStorage.getItem("aviary.nav.prefs")),
	).toBeNull();

	await page.reload();
	expect(await sidebarPages(page)).toEqual(DEFAULT_PAGES);
});

test("without the server's preferences, localStorage keeps them", async ({
	page,
}) => {
	await mockAgents(page, ["assistant"]);
	await page.route("/api/preferences/navigation", (route) =>
		route.fulfill({ status: 500, body: "unavailable" }),
	);
	await page.goto("/system/overview");
	await openEditor(page);
	await editRow(page, "agentPages", "channels")
		.getByTestId("nav-edit-visibility")
		.click();

	await page.reload();
	expect(await sidebarPages(page)).toEqual(
		DEFAULT_PAGES.filter((k) => k !== "channels"),
	);
});

test("a page missing from a saved order takes its default place", async ({
	page,
}) => {
	await mockAgents(page, ["assistant"]);
	// Saved by an older version: no Files page, plus one since removed.
	const saved = {
		order: {
			agentPages: ["settings", "tasks", "gone", "chat", "projects", "channels"],
		},
		hidden: {},
	};
	await page.route("/api/preferences/navigation", (route) =>
		route.fulfill({
			status: 200,
			contentType: "application/json",
			body: JSON.stringify(saved),
		}),
	);
	await page.goto("/system/overview");
	await expect
		.poll(() => sidebarPages(page))
		.toEqual(["settings", "tasks", "files", "chat", "projects", "channels"]);
});

/** The narrow-screen main row's keys, in order. */
function mobileRowKeys(page: Page) {
	return page
		.getByTestId("mobile-main-nav")
		.locator("a")
		.evaluateAll((els) => els.map((el) => el.getAttribute("data-key")));
}

test.describe("the top row", () => {
	test.use({ viewport: { width: 390, height: 844 } });

	test("follows the Pages order and visibility, with no order of its own", async ({
		page,
	}) => {
		await mockAgents(page, ["assistant"]);
		await page.goto("/agents/assistant/chat");
		await expect
			.poll(() => mobileRowKeys(page))
			.toEqual([...DEFAULT_PAGES, "system"]);

		await page.locator("header").getByTestId("gear-menu-button").click();
		await page.getByTestId("gear-menu-edit-nav").click();
		await expect(page.getByTestId("nav-edit-agentPages")).toBeVisible();
		await expect(page.getByTestId("nav-edit-top")).toHaveCount(0);
		await expect(page.getByTestId("nav-editor")).not.toContainText(
			/top buttons/i,
		);
		await editRow(page, "agentPages", "files")
			.getByTestId("nav-edit-up")
			.click();
		await editRow(page, "agentPages", "channels")
			.getByTestId("nav-edit-visibility")
			.click();

		await expect
			.poll(() => mobileRowKeys(page))
			.toEqual(["chat", "projects", "files", "tasks", "settings", "system"]);
		await page.setViewportSize({ width: 1280, height: 900 });
		expect(await sidebarPages(page)).toEqual([
			"chat",
			"projects",
			"files",
			"tasks",
			"settings",
		]);
	});

	test("drops a saved top-button order in favor of the Pages order", async ({
		page,
	}) => {
		await mockAgents(page, ["assistant", "coder"]);
		// Saved before the top row followed the Pages order (#1015).
		let saved: unknown = {
			order: {
				agentPages: ["tasks", "chat"],
				top: ["system", "agent:coder", "agent:assistant"],
			},
			hidden: { agentPages: ["files"], top: ["agent:coder"] },
		};
		const puts: unknown[] = [];
		await page.route("/api/preferences/navigation", async (route) => {
			if (route.request().method() === "PUT") {
				saved = route.request().postDataJSON();
				puts.push(saved);
			}
			await route.fulfill({
				status: 200,
				contentType: "application/json",
				body: JSON.stringify(saved),
			});
		});
		await page.addInitScript((prefs) => {
			localStorage.setItem("aviary.nav.prefs", JSON.stringify(prefs));
		}, saved);
		const errors: string[] = [];
		page.on("pageerror", (err) => errors.push(err.message));

		await page.goto("/agents/assistant/chat");
		// Agents keep their configured order, and System stays last.
		await expect
			.poll(() => mobileRowKeys(page))
			.toEqual(["agent:assistant", "agent:coder", "system"]);
		// The agent's own pages still follow its saved Pages order.
		await expect
			.poll(() =>
				page
					.getByTestId("mobile-sub-nav")
					.locator("a")
					.evaluateAll((els) =>
						els.map((el) => el.getAttribute("href")?.split("/").pop()),
					),
			)
			// Settings and the hidden Files, missing from the saved order, follow
			// Scheduled Tasks as they do by default.
			.toEqual(["tasks", "settings", "chat", "projects", "channels"]);
		await expect.poll(() => puts.length).toBe(1);
		expect(puts[0]).toEqual({
			order: { agentPages: ["tasks", "chat"] },
			hidden: { agentPages: ["files"] },
		});
		expect(
			JSON.parse(
				(await page.evaluate(() => localStorage.getItem("aviary.nav.prefs"))) ??
					"{}",
			),
		).toEqual(puts[0]);
		expect(errors).toEqual([]);
	});
});

test.describe("dragging", () => {
	test("lifts the row, opens a placeholder and settles it on drop", async ({
		page,
	}) => {
		await mockAgents(page, ["assistant"]);
		await page.goto("/system/overview");
		await openEditor(page);
		const files = editRow(page, "agentPages", "files");
		const projects = editRow(page, "agentPages", "projects");
		const before = await projects.boundingBox();
		const target = await editRow(page, "agentPages", "chat").boundingBox();
		await files.getByTestId("nav-edit-handle").hover();
		await page.mouse.down();
		await page.mouse.move((target?.x ?? 0) + 20, (target?.y ?? 0) + 30, {
			steps: 10,
		});

		const list = page.getByTestId("nav-edit-agentPages");
		const placeholder = list.getByTestId("sortable-placeholder");
		await expect(placeholder).toBeVisible();
		// Files lifts under the pointer while the rows it passed slide down.
		await expect(files).toHaveClass(/shadow-xl/);
		await expect
			.poll(async () => (await projects.boundingBox())?.y ?? 0)
			.toBeGreaterThan((before?.y ?? 0) + 10);
		// The placeholder opens where Code Loop was.
		await expect
			.poll(async () =>
				Math.abs(
					((await placeholder.boundingBox())?.y ?? 0) - (before?.y ?? 0),
				),
			)
			.toBeLessThan(2);
		// Nothing saves until the drop.
		expect(await rowKeys(page, "agentPages")).toEqual(DEFAULT_PAGES);

		await page.mouse.up();
		await expect(placeholder).toHaveCount(0);
		await expect
			.poll(() => rowKeys(page, "agentPages"))
			.toEqual(["chat", "files", "projects", "channels", "tasks", "settings"]);
		await expect(files).not.toHaveClass(/shadow-xl/);
	});

	test("Escape cancels a drag", async ({ page }) => {
		await mockAgents(page, ["assistant"]);
		await page.goto("/system/overview");
		await openEditor(page);
		const target = await editRow(page, "agentPages", "chat").boundingBox();
		await editRow(page, "agentPages", "settings")
			.getByTestId("nav-edit-handle")
			.hover();
		await page.mouse.down();
		await page.mouse.move((target?.x ?? 0) + 20, target?.y ?? 0, { steps: 8 });
		await page.keyboard.press("Escape");
		await page.mouse.up();
		// The editor stays, and the order is unchanged.
		await expect(page.getByTestId("nav-editor")).toBeVisible();
		await expect.poll(() => rowKeys(page, "agentPages")).toEqual(DEFAULT_PAGES);
	});

	test("the handle's arrow keys move its row", async ({ page }) => {
		await mockAgents(page, ["assistant"]);
		await page.goto("/system/overview");
		await openEditor(page);
		const handle = editRow(page, "agentPages", "chat").getByTestId(
			"nav-edit-handle",
		);
		await handle.focus();
		await page.keyboard.press("ArrowDown");
		await page.keyboard.press("ArrowDown");
		await expect(handle).toBeFocused();
		await page.keyboard.press("ArrowUp");
		await expect
			.poll(() => rowKeys(page, "agentPages"))
			.toEqual(["projects", "chat", "channels", "tasks", "files", "settings"]);
	});

	test("reorders without motion under prefers-reduced-motion", async ({
		page,
	}) => {
		await page.emulateMedia({ reducedMotion: "reduce" });
		await mockAgents(page, ["assistant"]);
		await page.goto("/system/overview");
		await openEditor(page);
		const files = editRow(page, "agentPages", "files");
		const target = await editRow(page, "agentPages", "chat").boundingBox();
		await files.getByTestId("nav-edit-handle").hover();
		await page.mouse.down();
		await page.mouse.move((target?.x ?? 0) + 20, (target?.y ?? 0) + 4, {
			steps: 8,
		});
		const motion = await files.evaluate((el) => {
			const s = getComputedStyle(el);
			return { scale: s.scale, rotate: s.rotate };
		});
		expect(motion).toEqual({ scale: "none", rotate: "none" });
		expect(
			await editRow(page, "agentPages", "chat").evaluate(
				(el) => getComputedStyle(el).transitionDuration,
			),
		).toBe("0s");
		await page.mouse.up();
		// The drop saves at once, with no settle.
		expect(await rowKeys(page, "agentPages")).toEqual([
			"files",
			"chat",
			"projects",
			"channels",
			"tasks",
			"settings",
		]);
	});

	test("autoscrolls the panel near its edges", async ({ page }) => {
		await page.setViewportSize({ width: 1280, height: 520 });
		await mockAgents(page, ["assistant"]);
		await page.goto("/system/overview");
		await openEditor(page);
		const scroller = page.getByTestId("settings-scroll");
		const box = await scroller.boundingBox();
		expect(
			await scroller.evaluate((el) => el.scrollHeight > el.clientHeight),
		).toBe(true);
		await editRow(page, "agentPages", "chat")
			.getByTestId("nav-edit-handle")
			.hover();
		await page.mouse.down();
		await page.mouse.move(
			(box?.x ?? 0) + 40,
			(box?.y ?? 0) + (box?.height ?? 0) - 6,
			{ steps: 10 },
		);
		await expect
			.poll(() => scroller.evaluate((el) => el.scrollTop))
			.toBeGreaterThan(20);
		await page.mouse.up();
		// Chat drops at the end of its own section.
		await expect
			.poll(async () => (await rowKeys(page, "agentPages")).at(-1))
			.toBe("chat");
	});
});

test.describe("dragging on touch", () => {
	test.use({
		hasTouch: true,
		isMobile: true,
		viewport: { width: 390, height: 844 },
	});

	test("a long press picks the row up; a swipe does not", async ({ page }) => {
		await mockAgents(page, ["assistant"]);
		await page.goto("/agents/assistant/chat");
		await page.locator("header").getByTestId("gear-menu-button").click();
		await page.getByTestId("gear-menu-edit-nav").click();
		await expect(page.getByTestId("nav-edit-agentPages")).toBeVisible();
		const cdp = await page.context().newCDPSession(page);
		const touch = (
			type: "touchStart" | "touchMove" | "touchEnd",
			x?: number,
			y?: number,
		) =>
			cdp.send("Input.dispatchTouchEvent", {
				type,
				touchPoints: x === undefined ? [] : [{ x, y: y ?? 0 }],
			});
		const label = async (key: string) => {
			const b = await editRow(page, "agentPages", key)
				.locator("span")
				.first()
				.boundingBox();
			return { x: (b?.x ?? 0) + 10, y: (b?.y ?? 0) + (b?.height ?? 0) / 2 };
		};

		// A quick swipe over a row moves nothing.
		let from = await label("tasks");
		let to = await label("chat");
		await touch("touchStart", from.x, from.y);
		for (let i = 1; i <= 6; i++)
			await touch("touchMove", from.x, from.y + ((to.y - from.y) * i) / 6);
		await touch("touchEnd");
		expect(await rowKeys(page, "agentPages")).toEqual(DEFAULT_PAGES);

		// A long press anywhere on the row picks it up. The swipe flung the
		// page, so let the fling settle, then bring the rows clear of the
		// autoscrolling edges.
		const scroller = page.getByTestId("settings-scroll");
		let last = -1;
		await expect
			.poll(async () => {
				const top = await scroller.evaluate((el) => el.scrollTop);
				const settled = top === last;
				last = top;
				return settled;
			})
			.toBe(true);
		await editRow(page, "agentPages", "chat").evaluate((el) =>
			el.scrollIntoView({ block: "center" }),
		);
		from = await label("tasks");
		to = await label("chat");
		await touch("touchStart", from.x, from.y);
		await page.waitForTimeout(500);
		const placeholder = page
			.getByTestId("nav-edit-agentPages")
			.getByTestId("sortable-placeholder");
		await expect(placeholder).toBeVisible();
		for (let i = 1; i <= 8; i++)
			await touch("touchMove", from.x, from.y + ((to.y - from.y - 4) * i) / 8);
		await touch("touchEnd");
		await expect(placeholder).toHaveCount(0);
		await expect
			.poll(() => rowKeys(page, "agentPages"))
			.toEqual(["tasks", "chat", "projects", "channels", "files", "settings"]);
	});
});

/** A section's rows in the sidebar editor, by key, in order. */
function rowKeys(page: Page, list: string): Promise<string[]> {
	return page
		.getByTestId(`nav-edit-${list}`)
		.getByTestId("nav-edit-row")
		.evaluateAll((els) => els.map((el) => el.getAttribute("data-key") ?? ""));
}
