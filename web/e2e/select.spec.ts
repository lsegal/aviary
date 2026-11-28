import { expect, type Locator, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

// The shared AppSelect/AppCombobox (web/src/components/ui/select) on a
// phone. The rest of its scenarios are component tests
// (web/src/scenarios/select.component.test.ts); this one stays here because
// its touch-sized targets come from `@media (pointer: coarse)`, which only a
// touch-enabled browser context (hasTouch/isMobile) matches, and Vitest's
// single shared browser page can't emulate that.

const TASK = {
	name: "daily-briefing",
	trigger: "daily",
	at: "09:00",
	prompt: "Summarize updates",
};

async function openSettings(page: Page) {
	await setAuthToken(page);
	await mockMCP(page, {
		config_get: {
			server: { port: 16677 },
			agents: [{ name: "assistant", model: "x/y", tasks: [TASK] }],
			models: {
				providers: {},
				defaults: { model: "", fallbacks: [] },
			},
			browser: { binary: "" },
			scheduler: { concurrency: "" },
			skills: {},
		},
		config_save: () => ({}),
		skills_list: [],
		tool_list: [],
		auth_list: [],
		session_list: [],
		agent_list: [{ id: "a1", name: "assistant", model: "x/y", state: "idle" }],
		agent_file_list: [],
		provider_cli_list: {
			clis: [
				{
					name: "codex",
					installed: true,
					binary: "codex",
					models: ["codex/gpt-5.5", "codex/gpt-5.5-mini", "codex/o4"],
					status: "ok",
				},
			],
		},
		task_preview: { description: "preview", next: [] },
	});
	await page.goto("/system/settings");
}

/** Waits for locator's open animation to finish, so it is measured at full size. */
function settled(locator: Locator) {
	return locator.evaluate((el) =>
		Promise.all(el.getAnimations().map((a) => a.finished)),
	);
}

function box(locator: Locator) {
	return locator.evaluate((el) => {
		const r = el.getBoundingClientRect();
		return {
			top: r.top,
			bottom: r.bottom,
			left: r.left,
			right: r.right,
			width: r.width,
			radius: getComputedStyle(el).borderTopLeftRadius,
		};
	});
}

test("dropdowns fit a phone screen with touch-sized targets", async ({
	browser,
}) => {
	const context = await browser.newContext({
		viewport: { width: 390, height: 844 },
		hasTouch: true,
		isMobile: true,
	});
	const page = await context.newPage();
	await openSettings(page);
	const trigger = page.getByTestId("approval-mode");
	expect(
		(await box(trigger)).bottom - (await box(trigger)).top,
	).toBeGreaterThanOrEqual(44);
	await trigger.tap();
	const content = page.getByTestId("select-content");
	await expect(content).toBeVisible();
	await settled(content);
	const popover = await box(content);
	expect(popover.left).toBeGreaterThanOrEqual(0);
	expect(popover.right).toBeLessThanOrEqual(390);
	const option = content.getByRole("option").first();
	expect(
		(await box(option)).bottom - (await box(option)).top,
	).toBeGreaterThanOrEqual(40);
	await context.close();
});
