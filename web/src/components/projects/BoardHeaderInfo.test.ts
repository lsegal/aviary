import { expect, it } from "vitest";
import { createSSRApp, h } from "vue";
import { renderToString } from "vue/server-renderer";
import type { ProjectStatus } from "../../lib/projects";
import BoardHeaderInfo from "./BoardHeaderInfo.vue";

// #1570: the Code Loop header shows a tag for its instance ID and an info
// icon for its details in place of the badge and subtitle.

const STATUS = {
	instanceID: "7F3CA91B",
	issueFilter: "assignee:@me",
	issueFilters: {},
	customFilters: [],
	delivery: "poll",
	lastChecked: new Date().toISOString(),
	interval: "30s",
	stale: false,
	cachedAt: "",
} as unknown as ProjectStatus;

const render = (status: ProjectStatus, showClis = false) =>
	renderToString(
		createSSRApp({
			render: () =>
				h(BoardHeaderInfo, {
					status,
					manual: false,
					filterTarget: "o/r",
					now: Date.now(),
					showClis,
				}),
		}),
	);

it("renders a named tag and info icon that open dialogs", async () => {
	const html = await render(STATUS);
	expect(html).toContain('data-testid="instance-tag"');
	expect(html).toContain('aria-label="Instance ID"');
	expect(html).toContain('data-testid="board-info"');
	expect(html).toContain('aria-label="Board details"');
	expect(html.match(/aria-haspopup="dialog"/g)).toHaveLength(2);
	expect(html.match(/aria-expanded="false"/g)).toHaveLength(2);
	// The icon forms show neither the badge nor the summary.
	expect(html).not.toContain('data-testid="instance-badge"');
	expect(html).not.toContain('data-testid="board-meta"');
});

// #1584: where the header has room, the same popovers open from the inline
// badge and summary, named by their text, and hidden copies of both are
// always rendered for the header to measure.
it("renders the inline badge and summary as the popovers' triggers", async () => {
	const html = await renderToString(
		createSSRApp({
			render: () =>
				h(BoardHeaderInfo, {
					status: STATUS,
					manual: false,
					filterTarget: "o/r",
					now: Date.now(),
					instanceInline: true,
					infoInline: true,
				}),
		}),
	);
	expect(html).toContain('data-testid="instance-badge"');
	expect(html).toContain('data-testid="board-meta"');
	expect(html).not.toContain('aria-label="Instance ID"');
	expect(html).not.toContain('aria-label="Board details"');
	expect(html.match(/aria-haspopup="dialog"/g)).toHaveLength(2);
	expect(html).toContain("Issues matching assignee:@me · poll · every 30s");
	for (const id of [
		"instance-badge-measure",
		"board-meta-measure",
		"header-icon-measure",
	])
		expect(html).toContain(`data-testid="${id}"`);
});

it("has no tag without an instance ID", async () => {
	const html = await render({ ...STATUS, instanceID: "" });
	expect(html).not.toContain('data-testid="instance-tag"');
	expect(html).toContain('data-testid="board-info"');
});

// #1580: on a phone the agent models and quota live in the details popover, and the
// closed icon warns, with a description, when one of them has a problem.
const CLIS = [
	{ name: "claude", models: ["opus"], quota: "week 70% left", signedIn: true },
	{ name: "gemini", models: [], quota: "signed out", signedIn: false },
];

it("marks the details icon when a CLI provider has a problem", async () => {
	const html = await render({ ...STATUS, clis: CLIS }, true);
	expect(html).toContain('data-testid="board-info-alert"');
	expect(html).toMatch(/aria-describedby="[^"]+"/);
	expect(html).toContain("CLI provider needs attention: gemini signed out");
});

it("has no warning when every provider is fine or on desktop", async () => {
	const fine = await render({ ...STATUS, clis: CLIS.slice(0, 1) }, true);
	expect(fine).not.toContain("board-info-alert");
	expect(fine).not.toContain("aria-describedby");
	const desktop = await render({ ...STATUS, clis: CLIS }, false);
	expect(desktop).not.toContain("board-info-alert");
});
