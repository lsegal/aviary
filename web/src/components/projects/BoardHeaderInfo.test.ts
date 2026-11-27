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

const render = (status: ProjectStatus) =>
	renderToString(
		createSSRApp({
			render: () =>
				h(BoardHeaderInfo, {
					status,
					manual: false,
					filterTarget: "o/r",
					now: Date.now(),
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
	// Neither the badge nor the subtitle shows inline any more.
	expect(html).not.toContain("7F3CA91B");
	expect(html).not.toContain("assignee:@me");
});

it("has no tag without an instance ID", async () => {
	const html = await render({ ...STATUS, instanceID: "" });
	expect(html).not.toContain('data-testid="instance-tag"');
	expect(html).toContain('data-testid="board-info"');
});
