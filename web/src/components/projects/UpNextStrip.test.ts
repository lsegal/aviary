import { expect, it } from "vitest";
import { createSSRApp, h } from "vue";
import { renderToString } from "vue/server-renderer";
import type { UpcomingRun } from "../../lib/projects";
import UpNextStrip from "./UpNextStrip.vue";

// #1577: Up next shows only runs due within the next five minutes, one chip
// per task, and is hidden when nothing is due that soon.

const NOW = Date.parse("2026-11-27T10:00:00Z");
const due = (name: string, minutes: number): UpcomingRun => ({
	id: `a/${name}`,
	name,
	project: "o/r",
	at: new Date(NOW + minutes * 60_000).toISOString(),
});

const render = (runs: UpcomingRun[], now: number) =>
	renderToString(createSSRApp({ render: () => h(UpNextStrip, { runs, now }) }));

const chips = (html: string) =>
	html.match(/data-testid="up-next-run"/g)?.length ?? 0;

it("shows a run due in 3 minutes and not one due in 10", async () => {
	const html = await render([due("soon", 3), due("later", 10)], NOW);
	expect(html).toContain('data-testid="up-next"');
	expect(chips(html)).toBe(1);
	expect(html).toContain("soon");
	expect(html).not.toContain("later");
});

it("shows the 10-minute run once now moves it within the window", async () => {
	const runs = [due("later", 10)];
	expect(await render(runs, NOW)).not.toContain('data-testid="up-next"');
	const html = await render(runs, NOW + 6 * 60_000);
	expect(chips(html)).toBe(1);
	expect(html).toContain("later");
});

it("drops a run once it fires", async () => {
	const runs = [due("soon", 1)];
	expect(chips(await render(runs, NOW))).toBe(1);
	expect(await render(runs, NOW + 61_000)).not.toContain(
		'data-testid="up-next"',
	);
});

it("renders nothing when no run is due within the window", async () => {
	expect(await render([due("later", 10)], NOW)).not.toContain("Up next");
	expect(await render([], NOW)).not.toContain("Up next");
});
