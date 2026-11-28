import { expect, it } from "vitest";
import { createSSRApp, h } from "vue";
import { renderToString } from "vue/server-renderer";
import type { CLIStatus } from "../../lib/projects";
import CLIProviderList from "./CLIProviderList.vue";

// #1582: the board's list of agent models and their quota is labelled for
// what it shows, not "CLI providers".

const CLIS = [
	{ name: "claude", models: ["opus"], quota: "week 65% left", signedIn: true },
	{ name: "gemini", models: [], quota: "", signedIn: false },
] as unknown as CLIStatus[];

const render = (props: {
	clis: readonly CLIStatus[];
	stacked?: boolean;
	testid?: string;
}) => renderToString(createSSRApp({ render: () => h(CLIProviderList, props) }));

it("labels the list as the agents' models and quota", async () => {
	const html = await render({ clis: CLIS, testid: "board-info-clis" });
	expect(html).toContain('aria-label="Models and quota"');
	expect(html).toContain(">Models &amp; quota</span>");
	expect(html).not.toContain("CLI providers");
	expect(html).toContain('data-testid="board-info-clis"');
	expect(html.match(/data-testid="cli-provider"/g)).toHaveLength(2);
	expect(html).toContain("week 65% left");
	expect(html).toContain("no quota reported");
});

it("stacks one agent per line in the details", async () => {
	const html = await render({ clis: CLIS, stacked: true });
	expect(html).toContain("flex-col items-start");
});
