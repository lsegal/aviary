// Shared by the settings-mobile-overflow*.component.test.ts scenarios
// (#997, #1093, #1636).

import { expect } from "vitest";
import { page } from "vitest/browser";
import { mockMCP } from "../component-test/server";

const NOW = new Date().toISOString();
const LONG = "the deploy failed again after the rollback finished ".repeat(6);

/** Mocks agent "Aviary" with channel "signal", a task, a project and long workflow previews. */
export function mockSettings(): void {
	const config = {
		agents: [
			{
				name: "Aviary",
				model: "x/y",
				channels: [
					{ type: "signal", id: "signal", deliver_to: "+15550001111" },
				],
				tasks: [{ name: "release-aviary", every: "1h", prompt: "release" }],
				projects: [{ target: "o/r" }],
			},
		],
	};
	mockMCP({
		agent_list: [{ id: "Aviary", name: "Aviary", model: "x/y", state: "idle" }],
		config_get: () => config,
		config_validate: [],
		auth_list: [],
		tool_list: [],
		job_list: [],
		job_query: [],
		task_list: [],
		session_list: [
			{ id: "main", agent_id: "Aviary", name: "main", created_at: NOW },
		],
		session_messages: [],
		channel_sessions: [
			{
				id: "channel:signal:signal",
				agent_id: "Aviary",
				name: "channel:signal:signal",
				created_at: NOW,
				own: true,
			},
		],
		workflow_entries: [{ entry: "+15550001111", title: "Alice" }],
		workflow_preview: [
			{
				entry: "+15550001111",
				sender: "Alice",
				text: LONG,
				timestamp: NOW,
				matches: true,
			},
			{
				entry: "+15550001111",
				sender: "Bob",
				text: "https://example.com/builds/1234567890/artifacts/logs/stderr-without-any-spaces.txt",
				timestamp: NOW,
				matches: true,
			},
		],
	});
}

/** Asserts neither the document nor the settings scroller scrolls sideways. */
export async function expectNoHorizontalOverflow(label: string): Promise<void> {
	await expect.element(page.getByTestId("settings-header")).toBeVisible();
	const scroller = document.querySelector("[data-testid=settings-scroll]");
	const sizes = {
		page: document.documentElement.scrollWidth - window.innerWidth,
		scroller: scroller ? scroller.scrollWidth - scroller.clientWidth : 0,
	};
	expect(sizes, label).toEqual({ page: 0, scroller: 0 });
	await expect
		.element(page.getByTestId("settings-back"))
		.toBeInViewport({ ratio: 1 });
	await expect.element(page.getByTestId("settings-title")).toBeInViewport();
}
