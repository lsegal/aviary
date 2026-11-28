import { createPinia } from "pinia";
import { expect, it } from "vitest";
import { type Component, createSSRApp, h } from "vue";
import { createMemoryHistory, createRouter } from "vue-router";
import { renderToString } from "vue/server-renderer";
import { normalizeJob, shortProjectName } from "../../lib/projects";
import JobCard from "./JobCard.vue";
import KanbanBoard from "./KanbanBoard.vue";

// #1647: on the all-projects board every card names its project, as a chip
// that opens the project's board; a single project's board leaves it out.

const job = (target: string, number: number) =>
	normalizeJob({
		target,
		number,
		title: `Job ${number}`,
		state: "running",
		status: "active",
		kind: "issue",
	});

const render = async (component: Component, props: Record<string, unknown>) => {
	const router = createRouter({
		history: createMemoryHistory(),
		routes: [{ path: "/:any(.*)*", component: { render: () => null } }],
	});
	const app = createSSRApp({ render: () => h(component, props) });
	app.use(createPinia());
	app.use(router);
	await router.push("/");
	return renderToString(app);
};

it("names short projects by their repository", () => {
	expect(shortProjectName("lsegal/zvid")).toBe("zvid");
	expect(shortProjectName("https://github.com/lsegal/aviary/discussions")).toBe(
		"aviary",
	);
	expect(shortProjectName("https://github.com/orgs/acme/projects/3")).toBe(
		"acme #3",
	);
	expect(shortProjectName("solo")).toBe("solo");
});

it("shows each board-mode card's short project with the full name in its tooltip", async () => {
	const html = await render(KanbanBoard, {
		jobs: [job("lsegal/aviary", 1639), job("lsegal/zvid", 1656)],
		filters: ["running"],
		now: Date.now(),
		showProject: true,
	});
	expect(html.match(/data-testid="kanban-project"/g)).toHaveLength(2);
	expect(html).toMatch(/data-testid="kanban-project"[^>]*>zvid</);
	expect(html).toContain('title="lsegal/zvid · open its Code Loop board"');
	expect(html).toContain('href="/system/watches/lsegal%2Fzvid"');
});

it("leaves the project off a single project's board-mode cards", async () => {
	const html = await render(KanbanBoard, {
		jobs: [job("lsegal/zvid", 1656)],
		filters: ["running"],
		now: Date.now(),
	});
	expect(html).not.toContain('data-testid="kanban-project"');
});

it("shows a list-mode card's full project, shortened at phone widths", async () => {
	const html = await render(JobCard, {
		job: job("lsegal/zvid", 1656),
		now: Date.now(),
		showProject: true,
	});
	expect(html).toContain('data-testid="job-project"');
	expect(html).toContain('<span class="sm:hidden">zvid</span>');
	expect(html).toContain('<span class="hidden sm:inline">lsegal/zvid</span>');
	const single = await render(JobCard, {
		job: job("lsegal/zvid", 1656),
		now: Date.now(),
	});
	expect(single).not.toContain('data-testid="job-project"');
});
