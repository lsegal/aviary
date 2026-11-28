import { expect } from "vitest";
import { type Locator, page } from "vitest/browser";
import { currentURL } from "../component-test/app";
import { mockMCP, route } from "../component-test/server";

// Shared fixtures for codeloop-project-nav*.component.test.ts (#1636).
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

export const job = (target: string, number: number, status = "active") => ({
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

export function setup(jobs: () => ReturnType<typeof job>[], health?: object) {
	mockMCP({
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
			return { states, ...STATUS };
		},
		project_job_list: (args?: Record<string, unknown>) => {
			const list = (args?.kinds as string[] | undefined)?.includes("schedule")
				? []
				: jobs();
			return { jobs: list, total: list.length, offset: 0, limit: 50 };
		},
		task_list: TASKS,
	});
	// After mockMCP, whose own /api/health route it overrides.
	if (health) {
		route("/api/health", (r) =>
			r.fulfill({
				status: 200,
				contentType: "application/json",
				body: JSON.stringify(health),
			}),
		);
	}
}

export const pill = (project: string) =>
	page.locator(`[data-testid="project-nav-item"][data-project="${project}"]`);

const count = (loc: Locator) => loc.elements().length;

// Code Loop pages rely on the shared breadcrumb alone (#1066): no page
// draws its own "<agent> · Code Loop /" line above its title, and the
// shared one leads back to the board everywhere.
export const expectSharedNavOnly = async () => {
	// The project's title is the shared header's last crumb (#1064).
	const header = page.getByTestId("settings-header");
	await expect
		.element(header.getByTestId("settings-title"))
		.toHaveText("lsegal/forgo");
	await expect
		.poll(() => count(page.getByRole("navigation", { name: "Breadcrumb" })))
		.toBe(1);
	expect(count(page.getByText("coder · Code Loop"))).toBe(0);
	expect(count(header.getByText(/ \/$/))).toBe(0);
	await expect
		.element(page.getByTestId("settings-back"))
		.toHaveAccessibleName("Back to Code Loop");
};

export const backToBoard = async () => {
	await page.getByTestId("settings-back").click();
	await expect.poll(currentURL).toMatch(/\/agents\/coder\/projects(\?.*)?$/);
	await expect
		.element(page.getByRole("heading", { name: "Code Loop" }))
		.toBeVisible();
	await expect
		.poll(() => count(page.getByTestId("settings-header").getByText("coder /")))
		.toBe(0);
};
