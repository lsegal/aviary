import { expect, test } from "vitest";
import { cdp, commands, type Locator, page } from "vitest/browser";
import { renderApp, setAuthToken } from "../component-test/app";
import { mockMCP } from "../component-test/server";
import { expectValue, listbox, optionLabels } from "./helpers/select";

declare global {
	interface ImportMeta {
		/** Vite's import.meta.glob, which the app's own env.d.ts leaves out. */
		glob: import("vite").ImportGlobFunction;
	}
}

// The shared AppSelect/AppCombobox (web/src/components/ui/select) behind every
// dropdown in the app.

// Every app source file's text, keyed by its path under web/src. Scenario
// files (ported from web/e2e, outside the app's source) don't count.
const SOURCES = import.meta.glob<string>(
	["../**/*.{vue,ts,tsx}", "!./**", "!../**/*.component.test.ts"],
	{ query: "?raw", import: "default", eager: true },
);

test("no raw <select> or <datalist> is used outside the shared component", () => {
	const paths = Object.keys(SOURCES);
	expect(paths.length).toBeGreaterThan(100);
	const offenders = paths
		.filter((path) => !path.startsWith("../components/ui/select/"))
		.filter((path) => /<(select|datalist)[\s>]/i.test(SOURCES[path]))
		.map((path) => path.slice(3));
	expect(offenders).toEqual([]);
});

const TASK = {
	name: "daily-briefing",
	trigger: "daily",
	at: "09:00",
	prompt: "Summarize updates",
};

async function openSettings() {
	setAuthToken();
	mockMCP({
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
	await renderApp("/system/settings");
}

async function openTriggerEditor() {
	await openSettings();
	// A fresh load, as page.goto: navigating the mounted Settings view in
	// place can drop the ?task query once its data is cached.
	await renderApp("/agents/assistant/tasks?task=daily-briefing");
	return page.getByTestId("trigger-editor");
}

/** Waits for locator's open animation to finish, so it is measured at full size. */
async function settled(locator: Locator) {
	await expect.element(locator).toBeInTheDocument();
	await Promise.all(
		locator
			.element()
			.getAnimations()
			.map((a) => a.finished),
	);
}

async function box(locator: Locator) {
	await expect.element(locator).toBeInTheDocument();
	const el = locator.element();
	const r = el.getBoundingClientRect();
	return {
		top: r.top,
		bottom: r.bottom,
		left: r.left,
		right: r.right,
		width: r.width,
		radius: getComputedStyle(el).borderTopLeftRadius,
	};
}

const count = (loc: Locator) => () => loc.elements().length;

test("the trigger and its popover share a radius, and the popover is at least as wide", async () => {
	await openSettings();
	const trigger = page.getByTestId("approval-mode");
	await trigger.click();
	const content = page.getByTestId("select-content");
	await expect.element(content).toBeVisible();
	await settled(content);
	const t = await box(trigger);
	const c = await box(content);
	expect(c.radius).toBe(t.radius);
	expect(t.radius).not.toBe("0px");
	expect(c.width).toBeGreaterThanOrEqual(t.width - 0.5);
});

test("the chevron's right inset is the same across dropdowns", async () => {
	const editor = await openTriggerEditor();
	const fields = [
		editor.getByLabelText("If the last run is still going"),
		editor.getByLabelText("Runs missed while asleep or stopped"),
		// The timezone combobox's field box wraps its search input.
		editor.getByLabelText("Timezone").locator("xpath=.."),
	];
	const insets: number[] = [];
	for (const field of fields) {
		const f = await box(field);
		const chevron = await box(field.locator(".select-chevron"));
		insets.push(Math.round(f.right - chevron.right));
	}
	expect(insets[0]).toBeGreaterThanOrEqual(12);
	expect(new Set(insets).size).toBe(1);
});

test("keyboard navigation selects options, Escape closes, and focus returns to the trigger", async () => {
	const editor = await openTriggerEditor();
	const missed = editor.getByLabelText("Runs missed while asleep or stopped");
	await expectValue(missed, "skip");

	// Enter opens with the chosen option highlighted; arrows move, Enter picks.
	const highlighted = () =>
		listbox(missed).locator("[role=option][data-highlighted]");
	const open = async () => {
		await commands.keyPress("Enter");
		await expect.poll(count(highlighted())).toBe(1);
	};
	(missed.element() as HTMLElement).focus();
	await open();
	await expect.element(highlighted()).toHaveAttribute("data-value", "skip");
	await commands.keyPress("ArrowDown");
	await expect.element(highlighted()).toHaveAttribute("data-value", "run_once");
	await commands.keyPress("Enter");
	await expect.poll(count(listbox(missed))).toBe(0);
	await expectValue(missed, "run_once");
	await expect.element(missed).toHaveFocus();

	// Home and End jump to the ends.
	await open();
	await commands.keyPress("Home");
	await expect.element(highlighted()).toHaveAttribute("data-value", "skip");
	await commands.keyPress("Enter");
	await expectValue(missed, "skip");
	await open();
	await commands.keyPress("End");
	await expect.element(highlighted()).toHaveAttribute("data-value", "run_once");
	await commands.keyPress("Enter");
	await expectValue(missed, "run_once");

	// Type-ahead highlights the option starting with the typed text.
	await open();
	await commands.keyType("Skip");
	await expect.element(highlighted()).toHaveAttribute("data-value", "skip");
	await commands.keyPress("Enter");
	await expectValue(missed, "skip");

	// Escape closes without changing the value; focus goes back.
	await open();
	await commands.keyPress("ArrowDown");
	await expect.element(highlighted()).toHaveAttribute("data-value", "run_once");
	await commands.keyPress("Escape");
	await expect.poll(count(listbox(missed))).toBe(0);
	await expectValue(missed, "skip");
	await expect.element(missed).toHaveFocus();
});

test("the selected option shows a check and options describe themselves", async () => {
	const editor = await openTriggerEditor();
	const missed = editor.getByLabelText("Runs missed while asleep or stopped");
	await missed.click();
	const chosen = listbox(missed).getByRole("option", { name: "Skip them" });
	await expect.element(chosen).toHaveAttribute("aria-selected", "true");
	await expect.element(chosen.getByTestId("select-check")).toBeVisible();
	const other = listbox(missed).getByRole("option", {
		name: "Run once to catch up",
	});
	await expect.poll(count(other.getByTestId("select-check"))).toBe(0);
	await expect
		.element(other)
		.toHaveAccessibleDescription(
			"Runs a single missed occurrence after waking",
		);
});

test("open and close animate, unless reduced motion is requested", async () => {
	await openSettings();
	const trigger = page.getByTestId("approval-mode");
	await trigger.click();
	const content = page.getByTestId("select-content");
	await expect.element(content).toBeVisible();
	expect(getComputedStyle(content.element()).animationName).toBe(
		"app-popover-in",
	);
	await commands.keyPress("Escape");

	await commands.emulateMedia({ reducedMotion: "reduce" });
	await trigger.click();
	await expect.element(content).toBeVisible();
	expect(getComputedStyle(content.element()).animationName).toBe("none");
	// The chevron's rotation doesn't animate either.
	expect(
		getComputedStyle(trigger.locator(".select-chevron").element())
			.transitionProperty,
	).toBe("none");
});

test("the popover flips up near the bottom of the viewport and scrolls long lists", async () => {
	const editor = await openTriggerEditor();
	const zone = editor.getByLabelText("Timezone");
	// Shrink the viewport until the field sits at its very bottom.
	const width = window.innerWidth;
	for (let i = 0; i < 3; i++) {
		const { bottom } = await box(zone);
		await page.viewport(width, Math.ceil(bottom) + 6);
	}
	zone.element().scrollIntoView({ block: "nearest" });
	await zone.click();
	const content = page.getByTestId("select-content");
	await expect.element(content).toBeVisible();
	await expect.element(content).toHaveAttribute("data-side", "top");
	await settled(content);
	const field = await box(zone);
	const popover = await box(content);
	expect(popover.bottom).toBeLessThanOrEqual(field.top);

	// Hundreds of zones fit in a scrolling list rather than overflowing.
	const viewport = content
		.locator("[data-radix-combobox-viewport], [role=listbox]")
		.first();
	const el = viewport.element();
	expect(el.scrollHeight).toBeGreaterThan(el.clientHeight);
	expect(popover.top).toBeGreaterThanOrEqual(0);
});

test("comboboxes search timezones and models", async () => {
	const editor = await openTriggerEditor();
	const zone = editor.getByLabelText("Timezone");
	await zone.click();
	await zone.fill("toron");
	const texts = (loc: Locator) => () =>
		loc.elements().map((el) => el.textContent ?? "");
	await expect
		.poll(texts(listbox(zone).getByRole("option")))
		.toEqual(["America/Toronto"]);
	await commands.keyPress("ArrowDown");
	await commands.keyPress("Enter");
	await expectValue(zone, "America/Toronto");
	await expect.element(zone).toHaveValue("America/Toronto");

	// A search that matches nothing says so.
	await zone.fill("zzzz");
	await expect
		.element(listbox(zone))
		.toMatchTextContent("No matching timezones");
	await commands.keyPress("Escape");

	await renderApp("/system/settings");
	const model = page.getByTestId("default-model-selector").locator("input");
	expect(await optionLabels(model)).toEqual(
		expect.arrayContaining(["codex/gpt-5.5", "codex/gpt-5.5-mini", "codex/o4"]),
	);
	await model.click();
	await model.fill("mini");
	await expect.poll(count(listbox(model).getByRole("option"))).toBe(1);
	await commands.keyPress("ArrowDown");
	await commands.keyPress("Enter");
	await expect.element(model).toHaveValue("codex/gpt-5.5-mini");
});

test("dropdowns fit a phone screen with touch-sized targets", async () => {
	// The spec's touch context (hasTouch/isMobile): Chromium's touch
	// emulation makes `@media (pointer: coarse)` match, which sizes the
	// targets. Turned off again after, as the spec closed its context.
	await page.viewport(390, 844);
	await cdp().send("Emulation.setTouchEmulationEnabled", {
		enabled: true,
		maxTouchPoints: 5,
	});
	try {
		expect(matchMedia("(pointer: coarse)").matches).toBe(true);
		await openSettings();
		const trigger = page.getByTestId("approval-mode");
		const t = await box(trigger);
		expect(t.bottom - t.top).toBeGreaterThanOrEqual(44);
		await trigger.click();
		const content = page.getByTestId("select-content");
		await expect.element(content).toBeVisible();
		await settled(content);
		const popover = await box(content);
		expect(popover.left).toBeGreaterThanOrEqual(0);
		expect(popover.right).toBeLessThanOrEqual(390);
		const option = await box(content.getByRole("option").first());
		expect(option.bottom - option.top).toBeGreaterThanOrEqual(40);
	} finally {
		await cdp().send("Emulation.setTouchEmulationEnabled", { enabled: false });
	}
});
