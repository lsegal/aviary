import { beforeAll, expect, it } from "vitest";
import { createSSRApp, h } from "vue";
import { renderToString } from "vue/server-renderer";
import type { BoardFilter } from "../../lib/projects";
import BoardStateFilter from "./BoardStateFilter.vue";

// #1490: the board's state filters render as one segmented control, or as
// a "Filters" button when the toolbar folds them.

beforeAll(() => {
	globalThis.ResizeObserver ??= class {
		observe() {}
		unobserve() {}
		disconnect() {}
	} as unknown as typeof ResizeObserver;
});

const COUNTS: Record<BoardFilter, number | null> = {
	scheduled: 0,
	starting: 1,
	paused: 0,
	running: 2,
	done: 3,
	failed: 0,
};

const render = (filters: BoardFilter[], collapsed: boolean) =>
	renderToString(
		createSSRApp({
			render: () => h(BoardStateFilter, { filters, counts: COUNTS, collapsed }),
		}),
	);

it("renders one group of toggles with counts, tinting the selected ones", async () => {
	const html = await render(["running"], false);
	expect(html).toContain('role="group" aria-label="Job filter"');
	expect(html.match(/aria-pressed="true"/g)).toHaveLength(1);
	expect(html.match(/aria-pressed="false"/g)).toHaveLength(5);
	expect(html).toContain("bg-blue-100");
	// Empty, unselected states dim but stay buttons.
	expect(html.match(/opacity-60/g)).toHaveLength(3);
	expect(html).toContain(">Clear</button>");
	expect(html).not.toContain('data-testid="filter-toggle"');
});

it("hides Clear when nothing is selected", async () => {
	const html = await render([], false);
	expect(html).not.toContain(">Clear</button>");
	expect(html).not.toContain('aria-pressed="true"');
});

it("folds into a Filters button with the active count", async () => {
	const folded = await render(["running", "done"], true);
	expect(folded).toContain('data-testid="filter-toggle"');
	expect(folded).toContain("Filters · 2");
	expect(folded).toContain('aria-expanded="false"');
	// The panel stays closed until the button opens it.
	expect(folded).not.toContain('aria-label="Job filter"');
	expect(await render([], true)).toMatch(/>Filters<\/span>/);
});
