import { createPinia } from "pinia";
import { expect, it, vi } from "vitest";
import { createSSRApp, h } from "vue";
import { renderToString } from "vue/server-renderer";
import type { ProjectStatus } from "../../lib/projects";
import BoardDetailsList from "./BoardDetailsList.vue";

// #1570, #1576: the board details the header's popover and its "…" menu
// share.

const STATUS = {
	instanceID: "7F3CA91B",
	issueFilter: "assignee:@me",
	issueFilters: {},
	customFilters: [],
	delivery: "poll",
	lastChecked: new Date(Date.now() - 10_000).toISOString(),
	interval: "30s",
	stale: true,
	cachedAt: new Date(Date.now() - 60_000).toISOString(),
} as unknown as ProjectStatus;

const render = (props: Record<string, unknown>) => {
	// The filter editor's MCP client reads the saved token and looks up a
	// router this app has none of.
	vi.stubGlobal("localStorage", { getItem: () => null });
	vi.spyOn(console, "warn").mockImplementation(() => {});
	const app = createSSRApp({
		render: () =>
			h(BoardDetailsList, {
				status: STATUS,
				manual: false,
				filterTarget: "",
				now: Date.now(),
				...props,
			}),
	});
	app.use(createPinia());
	return renderToString(app);
};

it("lists the filter, mode, delivery, last check and poll interval", async () => {
	const html = await render({});
	expect(html).toContain('data-testid="board-subtitle"');
	expect(html).toContain("assignee:@me");
	expect(html).toMatch(/data-testid="board-mode"[^>]*>Watch</);
	expect(html).toMatch(/data-testid="board-delivery"[^>]*>poll</);
	expect(html).toContain('data-testid="stale-indicator"');
	expect(html).toMatch(/data-testid="board-interval"[^>]*>every 30s</);
});

it("offers the filter editor for one project's board", async () => {
	const html = await render({ filterTarget: "o/r" });
	expect(html).toContain('data-testid="issue-filter-edit"');
});

it("says a manual board watches no issues and hides delivery", async () => {
	const html = await render({ manual: true });
	expect(html).toContain("Manual: no issue watching");
	expect(html).toMatch(/data-testid="board-mode"[^>]*>Manual</);
	expect(html).not.toContain('data-testid="board-delivery"');
});
