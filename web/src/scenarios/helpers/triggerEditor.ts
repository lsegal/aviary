// Component-test helper, ported from the Playwright helper of the same name (#1636).
import { expect } from "vitest";
import type { Locator } from "vitest/browser";
import { chooseOption, expectValue, searchOption } from "./select";

/**
 * A trigger as set through the shared trigger editor (TriggerEditor.vue),
 * with the aviary.yaml task fields it saves as.
 */
export interface TriggerCase {
	label: "Once" | "Interval" | "Daily" | "Weekly" | "Cron";
	/** The editor's inputs for the trigger, by label. */
	inputs: Record<string, string>;
	days?: string[];
	/** The saved task's trigger fields; start_at is checked separately. */
	saved: Record<string, string>;
}

export const TRIGGER_CASES: TriggerCase[] = [
	{
		label: "Once",
		inputs: { "Start at": "2027-01-15T10:30" },
		saved: { trigger: "once" },
	},
	{
		label: "Interval",
		inputs: { Every: "45m" },
		saved: { trigger: "interval", every: "45m" },
	},
	{
		label: "Daily",
		inputs: { At: "07:15" },
		saved: { trigger: "daily", at: "07:15" },
	},
	{
		label: "Weekly",
		inputs: { At: "09:00" },
		days: ["mon", "wed"],
		saved: { trigger: "weekly", at: "09:00", days: "mon,wed" },
	},
	{
		label: "Cron",
		inputs: { "Cron expression": "0 * * * *" },
		saved: { schedule: "0 * * * *" },
	},
];

export const TIMEZONE = "America/Los_Angeles";

function previewItems(editor: Locator) {
	return editor.getByTestId("trigger-preview").locator("li").elements().length;
}

/** Sets c, a timezone and both run policies in the trigger editor. */
export async function fillTrigger(scope: Locator, c: TriggerCase) {
	const editor = scope.getByTestId("trigger-editor");
	await editor.getByRole("radio", { name: c.label, exact: true }).click();
	for (const [label, value] of Object.entries(c.inputs))
		await editor.getByLabelText(label, { exact: true }).fill(value);
	for (const d of c.days ?? [])
		await editor.getByRole("button", { name: d, exact: true }).click();
	await searchOption(editor.getByLabelText("Timezone"), TIMEZONE);
	await chooseOption(
		editor.getByLabelText("If the last run is still going"),
		"Skip this run",
	);
	await chooseOption(
		editor.getByLabelText("Runs missed while asleep or stopped"),
		"Run once to catch up",
	);
	await expect.poll(() => previewItems(editor)).toBe(3);
}

/** Expects the trigger editor to show c as fillTrigger set it. */
export async function expectTrigger(scope: Locator, c: TriggerCase) {
	const editor = scope.getByTestId("trigger-editor");
	await expect
		.element(editor.getByRole("radio", { name: c.label, exact: true }))
		.toHaveAttribute("aria-checked", "true");
	for (const [label, value] of Object.entries(c.inputs))
		await expect
			.element(editor.getByLabelText(label, { exact: true }))
			.toHaveValue(value);
	for (const d of c.days ?? [])
		await expect
			.element(editor.getByRole("button", { name: d, exact: true }))
			.toHaveAttribute("aria-pressed", "true");
	await expect.element(editor.getByLabelText("Timezone")).toHaveValue(TIMEZONE);
	await expectValue(
		editor.getByLabelText("If the last run is still going"),
		"skip",
	);
	await expectValue(
		editor.getByLabelText("Runs missed while asleep or stopped"),
		"run_once",
	);
	await expect.poll(() => previewItems(editor)).toBe(3);
}

/** Expects task to hold c's trigger fields, timezone and policies. */
export function expectSavedTrigger(
	task: Record<string, unknown> | undefined,
	c: TriggerCase,
) {
	expect(task).toMatchObject({
		...c.saved,
		timezone: TIMEZONE,
		overlap: "skip",
		missed: "run_once",
	});
	if (c.label === "Once")
		expect(Date.parse(String(task?.start_at))).toBe(
			Date.parse("2027-01-15T10:30"),
		);
	if (c.label === "Cron") expect(task?.trigger).toBeUndefined();
}
