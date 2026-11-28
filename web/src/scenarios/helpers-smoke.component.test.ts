import { expect, test } from "vitest";
import { page } from "vitest/browser";
import { currentURL, goto, renderApp } from "../component-test/app";
import { mockMCP } from "../component-test/server";
import { sawChecking, watchForChecking } from "./helpers/analysisCache";
import {
	expectHeaderOneLine,
	headerItemPlaces,
	headerSettled,
	openBoardInfo,
	openInstanceTag,
} from "./helpers/boardHeader";
import { boundingBoxOf } from "./helpers/box";
import { mockAviary, PAGES } from "./helpers/pageHeaderPages";
import { expectValue, optionLabels } from "./helpers/select";
import { expectSettingsHeader } from "./helpers/settingsHeader";
import {
	agentPageLink,
	openAgentPage,
	openGearMenuItem,
	openTaskSettings,
	systemList,
} from "./helpers/sidebar";
import {
	expectTrigger,
	fillTrigger,
	TRIGGER_CASES,
} from "./helpers/triggerEditor";
import { openChat } from "./helpers/voice";

test("sidebar helpers", async () => {
	mockAviary();
	await openAgentPage("tasks");
	await expect.poll(currentURL).toBe("/agents/Aviary/tasks");
	await expect.element(agentPageLink("Files")).toBeVisible();
	await expect.element(await systemList()).toBeVisible();
	await openTaskSettings("release-aviary");
	await expect.poll(currentURL).toMatch(/task=release-aviary/);
	await openGearMenuItem("Settings");
	await expect.poll(currentURL).toBe("/system/settings");
});

test("settings header on every page", async () => {
	mockAviary();
	for (const p of PAGES.slice(0, 3).concat(
		PAGES.filter((x) => x.compact).slice(0, 1),
	)) {
		await goto(p.url);
		await expect
			.element(page.getByTestId("settings-title"))
			.toHaveText(p.title);
		await expectSettingsHeader(p.back !== false, p.compact === true);
	}
});

test("trigger editor and select helpers", async () => {
	mockAviary();
	await renderApp("/agents/Aviary/tasks?task=release-aviary");
	const main = page.locator("main");
	const sendVia = main.getByRole("combobox", { name: "Send Via" });
	await expectValue(sendVia, "");
	const labels = await optionLabels(sendVia);
	expect(labels.length).toBeGreaterThan(0);
	const c = TRIGGER_CASES[3];
	await fillTrigger(main, c);
	await expectTrigger(main, c);
	const box = await boundingBoxOf(main);
	expect(box.width).toBeGreaterThan(0);
});

function mockBoard() {
	mockMCP({
		agent_list: [],
		config_get: {
			agents: [
				{
					name: "coder",
					model: "cli/claude/opus",
					projects: [{ target: "lsegal/aviary" }],
				},
			],
		},
		project_status: {
			identity: "FC88FDCA",
			running: true,
			targets: ["lsegal/aviary"],
			issue_counts: {},
			jobs: {},
			states: {},
			delivery: "browser",
			poll_interval: "20s",
			issue_filter: "assignee:@me",
			issue_filters: { "lsegal/aviary": "assignee:@me" },
			custom_filters: [],
			clis: [],
			recent_logs: "",
		},
		project_job_list: { jobs: [], total: 0, offset: 0, limit: 50 },
		project_task_list: { tasks: [] },
	});
}

test("board header helpers", async () => {
	mockBoard();
	await renderApp("/agents/coder/projects/lsegal%2Faviary");
	await headerSettled();
	const tag = await openInstanceTag();
	await expect.element(tag.getByTestId("instance-id")).toHaveText("FC88FDCA");
	const info = await openBoardInfo();
	await expect.element(info.getByTestId("issue-filter")).toHaveText(
		"assignee:@me",
	);
});

test("board header on a phone", async () => {
	await page.viewport(390, 844);
	mockBoard();
	await renderApp("/agents/coder/projects");
	await headerSettled();
	await expectHeaderOneLine();
	const { header, menu } = await headerItemPlaces();
	expect(header.length + menu.length).toBeGreaterThan(0);
	await openBoardInfo(true);
});

test("voice openChat", async () => {
	const runs: unknown[] = [];
	const { mic, input, send } = await openChat(runs);
	await expect.element(mic).toBeVisible();
	await input.fill("hi");
	await send.click();
	await expect.poll(() => runs.length).toBe(1);
});

test("watchForChecking", async () => {
	watchForChecking();
	expect(sawChecking()).toBe(false);
	const el = document.createElement("div");
	el.setAttribute("data-testid", "task-cost-feedback");
	document.body.append(el);
	el.setAttribute("data-state", "checking");
	await expect.poll(sawChecking).toBe(true);
});
