import { readdirSync, readFileSync, statSync } from "node:fs";
import { dirname, join, relative } from "node:path";
import { fileURLToPath } from "node:url";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";
import { expectValue, listbox, optionLabels } from "./helpers/select";
import { expect, type Locator, type Page, test } from "./helpers/test";

// The shared AppSelect/AppCombobox (web/src/components/ui/select) behind every
// dropdown in the app.

const SRC = join(dirname(fileURLToPath(import.meta.url)), "..", "src");
const SHARED = join(SRC, "components", "ui", "select");

function sourceFiles(dir: string): string[] {
	return readdirSync(dir).flatMap((name) => {
		const path = join(dir, name);
		if (statSync(path).isDirectory()) return sourceFiles(path);
		return /\.(vue|ts|tsx)$/.test(name) ? [path] : [];
	});
}

test("no raw <select> or <datalist> is used outside the shared component", () => {
	const offenders = sourceFiles(SRC)
		.filter((path) => !path.startsWith(SHARED))
		.filter((path) =>
			/<(select|datalist)[\s>]/i.test(readFileSync(path, "utf8")),
		)
		.map((path) => relative(SRC, path));
	expect(offenders).toEqual([]);
});

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

async function openTriggerEditor(page: Page) {
	await openSettings(page);
	await page.goto("/agents/assistant/tasks?task=daily-briefing");
	return page.getByTestId("trigger-editor");
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

test("the trigger and its popover share a radius, and the popover is at least as wide", async ({
	page,
}) => {
	await openSettings(page);
	const trigger = page.getByTestId("approval-mode");
	await trigger.click();
	const content = page.getByTestId("select-content");
	await expect(content).toBeVisible();
	await settled(content);
	const [t, c] = await Promise.all([box(trigger), box(content)]);
	expect(c.radius).toBe(t.radius);
	expect(t.radius).not.toBe("0px");
	expect(c.width).toBeGreaterThanOrEqual(t.width - 0.5);
});

test("the chevron's right inset is the same across dropdowns", async ({
	page,
}) => {
	const editor = await openTriggerEditor(page);
	const fields = [
		editor.getByLabel("If the last run is still going"),
		editor.getByLabel("Runs missed while asleep or stopped"),
		// The timezone combobox's field box wraps its search input.
		editor.getByLabel("Timezone").locator("xpath=.."),
	];
	const insets: number[] = [];
	for (const field of fields) {
		const [f, chevron] = await Promise.all([
			box(field),
			box(field.locator(".select-chevron")),
		]);
		insets.push(Math.round(f.right - chevron.right));
	}
	expect(insets[0]).toBeGreaterThanOrEqual(12);
	expect(new Set(insets).size).toBe(1);
});

test("keyboard navigation selects options, Escape closes, and focus returns to the trigger", async ({
	page,
}) => {
	const editor = await openTriggerEditor(page);
	const missed = editor.getByLabel("Runs missed while asleep or stopped");
	await expectValue(missed, "skip");

	// Enter opens with the chosen option highlighted; arrows move, Enter picks.
	const open = async () => {
		await page.keyboard.press("Enter");
		await expect(
			listbox(missed).locator("[role=option][data-highlighted]"),
		).toHaveCount(1);
	};
	const highlighted = () =>
		listbox(missed).locator("[role=option][data-highlighted]");
	await missed.focus();
	await open();
	await expect(highlighted()).toHaveAttribute("data-value", "skip");
	await page.keyboard.press("ArrowDown");
	await expect(highlighted()).toHaveAttribute("data-value", "run_once");
	await page.keyboard.press("Enter");
	await expect(listbox(missed)).toHaveCount(0);
	await expectValue(missed, "run_once");
	await expect(missed).toBeFocused();

	// Home and End jump to the ends.
	await open();
	await page.keyboard.press("Home");
	await expect(highlighted()).toHaveAttribute("data-value", "skip");
	await page.keyboard.press("Enter");
	await expectValue(missed, "skip");
	await open();
	await page.keyboard.press("End");
	await expect(highlighted()).toHaveAttribute("data-value", "run_once");
	await page.keyboard.press("Enter");
	await expectValue(missed, "run_once");

	// Type-ahead highlights the option starting with the typed text.
	await open();
	await page.keyboard.type("Skip");
	await expect(highlighted()).toHaveAttribute("data-value", "skip");
	await page.keyboard.press("Enter");
	await expectValue(missed, "skip");

	// Escape closes without changing the value; focus goes back.
	await open();
	await page.keyboard.press("ArrowDown");
	await expect(highlighted()).toHaveAttribute("data-value", "run_once");
	await page.keyboard.press("Escape");
	await expect(listbox(missed)).toHaveCount(0);
	await expectValue(missed, "skip");
	await expect(missed).toBeFocused();
});

test("the selected option shows a check and options describe themselves", async ({
	page,
}) => {
	const editor = await openTriggerEditor(page);
	const missed = editor.getByLabel("Runs missed while asleep or stopped");
	await missed.click();
	const chosen = listbox(missed).getByRole("option", { name: "Skip them" });
	await expect(chosen).toHaveAttribute("aria-selected", "true");
	await expect(chosen.getByTestId("select-check")).toBeVisible();
	const other = listbox(missed).getByRole("option", {
		name: "Run once to catch up",
	});
	await expect(other.getByTestId("select-check")).toHaveCount(0);
	await expect(other).toHaveAccessibleDescription(
		"Runs a single missed occurrence after waking",
	);
});

test("open and close animate, unless reduced motion is requested", async ({
	page,
}) => {
	await openSettings(page);
	const trigger = page.getByTestId("approval-mode");
	await trigger.click();
	const content = page.getByTestId("select-content");
	await expect(content).toBeVisible();
	expect(
		await content.evaluate((el) => getComputedStyle(el).animationName),
	).toBe("app-popover-in");
	await page.keyboard.press("Escape");

	await page.emulateMedia({ reducedMotion: "reduce" });
	await trigger.click();
	await expect(content).toBeVisible();
	expect(
		await content.evaluate((el) => getComputedStyle(el).animationName),
	).toBe("none");
	// The chevron's rotation doesn't animate either.
	expect(
		await trigger
			.locator(".select-chevron")
			.evaluate((el) => getComputedStyle(el).transitionProperty),
	).toBe("none");
});

test("the popover flips up near the bottom of the viewport and scrolls long lists", async ({
	page,
}) => {
	const editor = await openTriggerEditor(page);
	const zone = editor.getByLabel("Timezone");
	// Shrink the viewport until the field sits at its very bottom.
	const width = page.viewportSize()?.width ?? 1280;
	for (let i = 0; i < 3; i++) {
		const { bottom } = await box(zone);
		await page.setViewportSize({ width, height: Math.ceil(bottom) + 6 });
	}
	await zone.scrollIntoViewIfNeeded();
	await zone.click();
	const content = page.getByTestId("select-content");
	await expect(content).toBeVisible();
	await expect(content).toHaveAttribute("data-side", "top");
	await settled(content);
	const [field, popover] = await Promise.all([box(zone), box(content)]);
	expect(popover.bottom).toBeLessThanOrEqual(field.top);

	// Hundreds of zones fit in a scrolling list rather than overflowing.
	const viewport = content
		.locator("[data-radix-combobox-viewport], [role=listbox]")
		.first();
	const sizes = await viewport.evaluate((el) => ({
		scroll: el.scrollHeight,
		client: el.clientHeight,
	}));
	expect(sizes.scroll).toBeGreaterThan(sizes.client);
	expect(popover.top).toBeGreaterThanOrEqual(0);
});

test("comboboxes search timezones and models", async ({ page }) => {
	const editor = await openTriggerEditor(page);
	const zone = editor.getByLabel("Timezone");
	await zone.click();
	await zone.fill("toron");
	expect(await listbox(zone).getByRole("option").allTextContents()).toEqual([
		"America/Toronto",
	]);
	await page.keyboard.press("ArrowDown");
	await page.keyboard.press("Enter");
	await expectValue(zone, "America/Toronto");
	await expect(zone).toHaveValue("America/Toronto");

	// A search that matches nothing says so.
	await zone.fill("zzzz");
	await expect(listbox(zone)).toContainText("No matching timezones");
	await page.keyboard.press("Escape");

	await page.goto("/system/settings");
	const model = page.getByTestId("default-model-selector").locator("input");
	expect(await optionLabels(model)).toEqual(
		expect.arrayContaining(["codex/gpt-5.5", "codex/gpt-5.5-mini", "codex/o4"]),
	);
	await model.click();
	await model.fill("mini");
	await expect(listbox(model).getByRole("option")).toHaveCount(1);
	await page.keyboard.press("ArrowDown");
	await page.keyboard.press("Enter");
	await expect(model).toHaveValue("codex/gpt-5.5-mini");
});

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
