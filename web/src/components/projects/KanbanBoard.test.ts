// @vitest-environment happy-dom
import { mount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";
import type { ProjectJob } from "../../lib/projects";

vi.mock("../../stores/settings", async (orig) => ({
	...(await orig<typeof import("../../stores/settings")>()),
	useSettingsStore: () => ({ config: null }),
}));

const { default: KanbanBoard } = await import("./KanbanBoard.vue");

function job(n: number, state: ProjectJob["state"]): ProjectJob {
	return {
		id: `o/r#${n}`,
		target: "o/r",
		repo: "o/r",
		number: n,
		title: `Job ${n}`,
		state,
	} as ProjectJob;
}

// #1657: a long column, such as Done on a busy project, renders a page of
// cards at a time instead of hundreds at once.
describe("KanbanBoard", () => {
	it("pages a long column and shows more on demand", async () => {
		const jobs = [
			...Array.from({ length: 120 }, (_, i) => job(i + 1, "done")),
			job(500, "running"),
		];
		const wrapper = mount(KanbanBoard, {
			props: { jobs, filters: [], now: Date.now() },
			global: { stubs: { RouterLink: { template: "<a><slot /></a>" } } },
		});
		const column = (state: string) =>
			wrapper.find(`[data-testid="kanban-column"][data-state="${state}"]`);
		const cards = (state: string) =>
			column(state).findAll('[data-testid="kanban-card"]');
		expect(cards("done")).toHaveLength(50);
		// The column's count still covers every job.
		expect(column("done").find("h3").text()).toContain("120");
		expect(cards("running")).toHaveLength(1);
		expect(
			column("running").find('[data-testid="kanban-show-more"]').exists(),
		).toBe(false);

		const more = column("done").find('[data-testid="kanban-show-more"]');
		expect(more.text()).toContain("Show 50 more");
		await more.trigger("click");
		expect(cards("done")).toHaveLength(100);
		await column("done")
			.find('[data-testid="kanban-show-more"]')
			.trigger("click");
		expect(cards("done")).toHaveLength(120);
		expect(
			column("done").find('[data-testid="kanban-show-more"]').exists(),
		).toBe(false);
	});
});
