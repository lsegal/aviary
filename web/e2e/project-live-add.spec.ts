import { expect, type Page, test, type WebSocketRoute } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";
import { targetJobs } from "./helpers/targetJobs";

// A project added, removed or polled shows in every view at once: in the
// adding tab before project_add answers, and in every other client through
// the project events on /api/ws (#1211). The project lists poll every 5 to
// 10 s, so each check here waits well under one poll.
const WITHIN_A_RENDER = { timeout: 2_000 };

const READY_CHECK = {
	target: "lsegal/zvidlib",
	kind: "repo",
	ok: true,
	checks: [
		{
			name: "github",
			status: "ok",
			message: "gh is signed in and can read lsegal/zvidlib",
		},
	],
};

type Status = {
	targets: string[];
	issue_counts: Record<string, number>;
	loading_targets?: string[];
};

interface Server {
	status: Status;
	jobs: object[];
	sockets: WebSocketRoute[];
	/** Settles the pending project_add, failing it with an error message. */
	finishAdd: (error?: string) => void;
	send: (event: object) => void;
}

async function setup(page: Page): Promise<Server> {
	await setAuthToken(page);
	const server: Server = {
		status: {
			targets: ["lsegal/aviary"],
			issue_counts: { "lsegal/aviary": 2 },
		},
		jobs: [],
		sockets: [],
		finishAdd: () => {},
		send: (event) => {
			for (const ws of server.sockets) ws.send(JSON.stringify(event));
		},
	};
	const config: {
		server: object;
		agents: { name: string; model: string; projects?: { target: string }[] }[];
	} = {
		server: { port: 16677 },
		agents: [
			{ name: "atlas", model: "anthropic/claude-sonnet-4-5" },
			{
				name: "coder",
				model: "cli/claude/opus",
				projects: [{ target: "lsegal/aviary" }],
			},
		],
	};
	await page.routeWebSocket(/\/api\/ws/, (ws) => {
		server.sockets.push(ws);
		ws.send(JSON.stringify({ type: "health", ok: true, version: "v1" }));
	});
	await mockMCP(page, {
		agent_list: [],
		auth_list: [],
		session_list: [],
		tool_list: [],
		agent_file_list: [],
		task_list: [],
		config_validate: [],
		config_get: () => config,
		project_status: () => ({
			...server.status,
			target_jobs: targetJobs(server.jobs as { target: string }[]),
		}),
		project_job_list: () => ({
			jobs: server.jobs,
			total: server.jobs.length,
		}),
		project_check: () => READY_CHECK,
		provider_cli_list: { clis: [] },
		// project_add answers only when the test says so.
		project_add: () =>
			new Promise((resolve, reject) => {
				server.finishAdd = (error) => {
					if (error) return reject(new Error(error));
					// Saved to coder's projects, as the server saves it.
					config.agents[1].projects?.push({ target: "lsegal/zvidlib" });
					resolve({ saved: true });
				};
			}),
	});
	return server;
}

const sidebarProject = (page: Page, name: string) =>
	page
		.getByTestId("sidebar-agent-projects")
		.getByRole("link", { name: new RegExp(name.replace("/", "\\/")) });

async function startAdding(page: Page) {
	await page.goto("/agents/coder/projects/lsegal%2Faviary?add=1");
	const dialog = page.getByRole("dialog", { name: "Watch a repository" });
	await dialog.getByLabel("Repository or project board").fill("lsegal/zvidlib");
	await expect(dialog.getByTestId("add-project-checks")).toContainText(
		"Ready to go",
	);
	await expect(sidebarProject(page, "lsegal/aviary")).toBeVisible();
	await dialog.getByRole("button", { name: "Start watching" }).click();
	return dialog;
}

test("an added project shows in the sidebar at once, checking GitHub", async ({
	page,
}) => {
	const server = await setup(page);
	await startAdding(page);

	// project_add has not answered and the server lists nothing new, yet the
	// sidebar already shows the project under its agent.
	const zvidlib = sidebarProject(page, "lsegal/zvidlib");
	await expect(zvidlib).toBeVisible(WITHIN_A_RENDER);
	await expect(zvidlib.getByTestId("project-loading-badge")).toHaveAttribute(
		"aria-label",
		"Checking GitHub",
	);

	// The server lists it, still loading, once the add lands.
	server.status = {
		targets: ["lsegal/aviary", "lsegal/zvidlib"],
		issue_counts: { "lsegal/aviary": 2 },
		loading_targets: ["lsegal/zvidlib"],
	};
	server.finishAdd();
	await expect(page).toHaveURL("/agents/coder/projects/lsegal%2Fzvidlib");
	await expect(zvidlib.getByTestId("project-loading-badge")).toBeVisible();
});

test("a failed add takes the project back out and says why", async ({
	page,
}) => {
	const server = await setup(page);
	const dialog = await startAdding(page);
	const zvidlib = sidebarProject(page, "lsegal/zvidlib");
	await expect(zvidlib).toBeVisible(WITHIN_A_RENDER);

	server.finishAdd("gh cannot read lsegal/zvidlib");
	await expect(zvidlib).toHaveCount(0, WITHIN_A_RENDER);
	await expect(dialog).toContainText("gh cannot read lsegal/zvidlib");
	await expect(sidebarProject(page, "lsegal/aviary")).toBeVisible();
});

test("another client's project events show without a poll", async ({
	page,
}) => {
	const server = await setup(page);
	await page.goto("/agents/coder/projects");
	await expect(sidebarProject(page, "lsegal/aviary")).toBeVisible();
	await expect(page.getByTestId("project-nav-item")).toHaveCount(1);
	await expect.poll(() => server.sockets.length).toBeGreaterThan(0);

	// Added elsewhere: the project shows in the sidebar and the Code Loop
	// root's project tabs before any read lists it.
	server.send({
		type: "project-added",
		target: "lsegal/zvidlib",
		agent_id: "coder",
	});
	const zvidlib = sidebarProject(page, "lsegal/zvidlib");
	await expect(zvidlib.getByTestId("project-loading-badge")).toBeVisible(
		WITHIN_A_RENDER,
	);
	const tab = page.locator(
		'[data-testid="project-nav-item"][data-project="lsegal/zvidlib"]',
	);
	await expect(tab.getByTestId("project-nav-count")).toHaveText(
		"· checking GitHub…",
		WITHIN_A_RENDER,
	);

	// Its first poll lands: the counts replace the loading state.
	server.status = {
		targets: ["lsegal/aviary", "lsegal/zvidlib"],
		issue_counts: { "lsegal/aviary": 2, "lsegal/zvidlib": 4 },
	};
	server.jobs = [
		{
			key: "lsegal/zvidlib#7",
			target: "lsegal/zvidlib",
			number: 7,
			title: "Fix decoder",
			status: "active",
			state: "running",
		},
	];
	server.send({ type: "project-updated", target: "lsegal/zvidlib" });
	await expect(zvidlib.getByTestId("project-loading-badge")).toHaveCount(
		0,
		WITHIN_A_RENDER,
	);
	await expect(zvidlib.getByTestId("project-running-badge")).toHaveText("1");
	await expect(tab.getByTestId("project-nav-count")).toHaveText("· 1 running");

	// Removed elsewhere: it goes just as fast.
	server.send({
		type: "project-removed",
		target: "lsegal/zvidlib",
		agent_id: "coder",
	});
	await expect(zvidlib).toHaveCount(0, WITHIN_A_RENDER);
});
