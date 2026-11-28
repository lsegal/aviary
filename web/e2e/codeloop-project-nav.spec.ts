import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";
import { targetJobs } from "./helpers/targetJobs";

// The Code Loop board's project pills lead into each project and its
// scheduled tasks, so they stay reachable with the sidebar collapsed (#1044).
// Fixtures follow the payloads of project_status, project_job_list and
// task_list.

const TARGETS = ["lsegal/aviary", "lsegal/forgo"];

const STATUS = {
	running: true,
	targets: TARGETS,
	issue_counts: { "lsegal/aviary": 6, "lsegal/forgo": 0 },
	delivery: "poll",
	clis: [{ name: "claude", models: ["opus"], quota: "week 26% left" }],
	recent_logs: "",
};

const job = (target: string, number: number, status = "active") => ({
	key: `${target}#${number}`,
	target,
	number,
	title: `Issue ${number}`,
	status,
	state: status === "active" ? "running" : "paused",
	kind: "issue",
	agent: "claude",
	model: "opus",
	repository: target,
	actions: { retry: false, pause: true, resume: false, stop: true },
	started: new Date().toISOString(),
});

const TASKS = [
	{
		id: "coder/nightly-triage",
		name: "nightly-triage",
		agent_name: "coder",
		type: "project",
		project: "lsegal/forgo",
		prompt: "Triage new issues.",
		enabled: true,
		trigger_type: "daily",
		at: "02:00",
		created_at: "0001-01-01T00:00:00Z",
	},
];

async function setup(
	page: Page,
	jobs: () => ReturnType<typeof job>[],
	health?: object,
) {
	if (health) {
		await page.addInitScript(() => localStorage.removeItem("aviary_token"));
		await page.route("/api/health", (route) =>
			route.fulfill({
				status: 200,
				contentType: "application/json",
				body: JSON.stringify(health),
			}),
		);
	} else {
		await setAuthToken(page);
	}
	await mockMCP(page, {
		agent_list: [{ name: "coder" }],
		job_list: [],
		task_upcoming: [],
		config_validate: [],
		config_get: {
			agents: [
				{
					name: "coder",
					model: "cli/claude/opus",
					projects: TARGETS.map((target) => ({ target })),
				},
			],
		},
		project_status: () => {
			const states = { starting: 0, paused: 0, running: 0, done: 0, failed: 0 };
			for (const j of jobs())
				states[j.state as keyof typeof states] =
					(states[j.state as keyof typeof states] ?? 0) + 1;
			return { states, target_jobs: targetJobs(jobs()), ...STATUS };
		},
		project_job_list: (args?: Record<string, unknown>) => {
			const list = (args?.kinds as string[] | undefined)?.includes("schedule")
				? []
				: jobs();
			return { jobs: list, total: list.length, offset: 0, limit: 50 };
		},
		task_list: TASKS,
	});
}

const pill = (page: Page, project: string) =>
	page.locator(`[data-testid="project-nav-item"][data-project="${project}"]`);

test("with the sidebar collapsed, the board's pills open every project and its scheduled tasks", async ({
	page,
}) => {
	await setup(page, () => [job("lsegal/aviary", 1), job("lsegal/aviary", 2)]);
	await page.goto("/agents/coder/projects");
	await page.getByTestId("sidebar-collapse").click();

	// The pills sit left of the models and quota, in the same row.
	const nav = page.getByRole("navigation", { name: "Projects" });
	await expect(nav.getByTestId("project-nav-item")).toHaveCount(2);
	const providers = page.getByRole("region", { name: "Models and quota" });
	const navBox = await nav.boundingBox();
	const providersBox = await providers.boundingBox();
	expect(navBox && providersBox).toBeTruthy();
	if (navBox && providersBox) {
		expect(navBox.x + navBox.width).toBeLessThanOrEqual(providersBox.x + 1);
		expect(Math.abs(navBox.y - providersBox.y)).toBeLessThan(
			providersBox.height,
		);
	}

	const aviary = pill(page, "lsegal/aviary");
	await expect(aviary.getByTestId("project-nav-count")).toHaveText(
		"· 2 running",
	);
	await expect(aviary.getByTestId("project-nav-dot")).toHaveAttribute(
		"data-state",
		"running",
	);
	await expect(aviary.getByTestId("project-nav-scheduled")).toHaveCount(0);
	const forgo = pill(page, "lsegal/forgo");
	await expect(forgo.getByTestId("project-nav-count")).toHaveText("· 0");
	await expect(forgo.getByTestId("project-nav-scheduled")).toHaveText(
		"Scheduled 1",
	);

	// A project's pill opens its page, whose breadcrumb leads back.
	await aviary.getByTestId("project-nav-link").click();
	await expect(page).toHaveURL("/agents/coder/projects/lsegal%2Faviary");
	await expect(
		page.getByRole("heading", { name: "lsegal/aviary" }),
	).toBeVisible();
	await page
		.getByTestId("settings-breadcrumb")
		.getByRole("link", { name: "Code Loop", exact: true })
		.click();
	await expect(page).toHaveURL(/\/agents\/coder\/projects(\?.*)?$/);

	// Scheduled opens the project's scheduled tasks.
	await pill(page, "lsegal/forgo").getByTestId("project-nav-scheduled").click();
	await expect(page).toHaveURL(
		"/agents/coder/projects/lsegal%2Fforgo?state=scheduled",
	);
	await expect(page.getByText("nightly-triage").first()).toBeVisible();
});

// Code Loop pages rely on the shared breadcrumb alone (#1066): no page
// draws its own "<agent> · Code Loop /" line above its title, and the
// shared one leads back to the board everywhere.
const expectSharedNavOnly = async (page: Page) => {
	// The project's title is the shared header's last crumb (#1064).
	const header = page.getByTestId("settings-header");
	await expect(header.getByTestId("settings-title")).toHaveText("lsegal/forgo");
	await expect(
		page.getByRole("navigation", { name: "Breadcrumb" }),
	).toHaveCount(1);
	await expect(page.getByText("coder · Code Loop")).toHaveCount(0);
	await expect(header.getByText(/ \/$/)).toHaveCount(0);
	await expect(page.getByTestId("settings-back")).toHaveAccessibleName(
		"Back to Code Loop",
	);
};

const backToBoard = async (page: Page) => {
	await page.getByTestId("settings-back").click();
	await expect(page).toHaveURL(/\/agents\/coder\/projects(\?.*)?$/);
	await expect(page.getByRole("heading", { name: "Code Loop" })).toBeVisible();
	await expect(
		page.getByTestId("settings-header").getByText("coder /"),
	).toHaveCount(0);
};

test("a project page has no breadcrumb of its own, and the shared one leads back", async ({
	page,
}) => {
	await setup(page, () => []);
	await page.goto("/agents/coder/projects/lsegal%2Fforgo");
	await expectSharedNavOnly(page);
	await page
		.getByTestId("settings-breadcrumb")
		.getByRole("link", { name: "Code Loop", exact: true })
		.click();
	await expect(page).toHaveURL(/\/agents\/coder\/projects(\?.*)?$/);
	await page.goBack();
	await backToBoard(page);
});

test("with the sidebar collapsed the shared breadcrumb leads back to the board", async ({
	page,
}) => {
	await setup(page, () => []);
	await page.goto("/agents/coder/projects/lsegal%2Fforgo");
	await page.getByTestId("sidebar-collapse").click();
	await expect(page.getByTestId("sidebar-rail")).toBeVisible();
	await expectSharedNavOnly(page);
	await backToBoard(page);
});

test("in an aviary watch session the shared breadcrumb leads back to the board", async ({
	page,
}) => {
	await setup(page, () => [], {
		ok: true,
		version: "dev",
		auth_disabled: true,
		minimal: true,
	});
	await page.goto("/agents/coder/projects/lsegal%2Fforgo");
	await expectSharedNavOnly(page);
	await backToBoard(page);
});

test("a project's pill keeps the board's job filter", async ({ page }) => {
	await setup(page, () => []);
	await page.goto("/agents/coder/projects?state=running");
	await expect(
		pill(page, "lsegal/forgo").getByTestId("project-nav-link"),
	).toHaveAttribute(
		"href",
		"/agents/coder/projects/lsegal%2Fforgo?state=running",
	);
});

test("the pills' counts and colors follow jobs live", async ({ page }) => {
	await page.clock.install();
	let jobs = [job("lsegal/forgo", 1)];
	await setup(page, () => jobs);
	await page.goto("/agents/coder/projects");
	const forgo = pill(page, "lsegal/forgo");
	await expect(forgo.getByTestId("project-nav-count")).toHaveText(
		"· 1 running",
	);

	jobs = [job("lsegal/forgo", 1, "paused")];
	await page.clock.runFor(5_000);
	await expect(forgo.getByTestId("project-nav-count")).toHaveText(
		"· 1 waiting",
	);
	await expect(forgo.getByTestId("project-nav-dot")).toHaveAttribute(
		"data-state",
		"waiting",
	);

	jobs = [];
	await page.clock.runFor(5_000);
	await expect(forgo.getByTestId("project-nav-count")).toHaveText("· 0");
	await expect(forgo.getByTestId("project-nav-dot")).toHaveAttribute(
		"data-state",
		"idle",
	);
});

test.describe("on a phone", () => {
	test.use({ viewport: { width: 390, height: 844 } });

	test("the pills scroll sideways without overflowing the page", async ({
		page,
	}) => {
		await setup(page, () => [job("lsegal/aviary", 1)]);
		await page.goto("/agents/coder/projects");
		const nav = page.getByRole("navigation", { name: "Projects" });
		await expect(nav.getByTestId("project-nav-item")).toHaveCount(2);
		const row = await nav.evaluate((el) => ({
			scroll: el.scrollWidth,
			client: el.clientWidth,
			wrap: getComputedStyle(el).whiteSpace,
		}));
		expect(row.wrap).toBe("nowrap");
		expect(row.scroll).toBeGreaterThan(row.client);
		// Neither the page nor any scroller around the row scrolls sideways.
		const overflowing = await nav.evaluate((el) => {
			const wide: string[] = [];
			for (let a = el.parentElement; a; a = a.parentElement)
				if (a.scrollWidth > a.clientWidth + 1)
					wide.push(`${a.tagName}.${a.className}`);
			return wide;
		});
		expect(overflowing).toEqual([]);
	});

	test("a project page has no breadcrumb of its own, and the shared one leads back", async ({
		page,
	}) => {
		await setup(page, () => []);
		await page.goto("/agents/coder/projects/lsegal%2Fforgo");
		await expectSharedNavOnly(page);
		await expect(page.getByTestId("settings-back")).toBeInViewport();
		await backToBoard(page);
	});
});

test("an `aviary watch` session's Code Loop has the project pills", async ({
	page,
}) => {
	await setup(page, () => [job("lsegal/aviary", 1)], {
		ok: true,
		version: "dev",
		auth_disabled: true,
		minimal: true,
	});
	await page.goto("/agents/coder/projects");
	await pill(page, "lsegal/forgo").getByTestId("project-nav-scheduled").click();
	await expect(page).toHaveURL(
		"/agents/coder/projects/lsegal%2Fforgo?state=scheduled",
	);
	await expect(page.getByText("nightly-triage").first()).toBeVisible();
});
