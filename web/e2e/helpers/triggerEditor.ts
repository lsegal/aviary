import { expect, type Locator } from "@playwright/test";

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

/** Sets c, a timezone and both run policies in the trigger editor. */
export async function fillTrigger(scope: Locator, c: TriggerCase) {
	const editor = scope.getByTestId("trigger-editor");
	await editor.getByRole("radio", { name: c.label, exact: true }).click();
	for (const [label, value] of Object.entries(c.inputs))
		await editor.getByLabel(label, { exact: true }).fill(value);
	for (const d of c.days ?? [])
		await editor.getByRole("button", { name: d, exact: true }).click();
	await editor.getByLabel("Timezone").fill(TIMEZONE);
	await editor
		.getByLabel("If the last run is still going")
		.selectOption("skip");
	await editor
		.getByLabel("Runs missed while asleep or stopped")
		.selectOption("run_once");
	await expect(editor.getByTestId("trigger-preview").locator("li")).toHaveCount(
		3,
	);
}

/** Expects the trigger editor to show c as fillTrigger set it. */
export async function expectTrigger(scope: Locator, c: TriggerCase) {
	const editor = scope.getByTestId("trigger-editor");
	await expect(
		editor.getByRole("radio", { name: c.label, exact: true }),
	).toHaveAttribute("aria-checked", "true");
	for (const [label, value] of Object.entries(c.inputs))
		await expect(editor.getByLabel(label, { exact: true })).toHaveValue(value);
	for (const d of c.days ?? [])
		await expect(
			editor.getByRole("button", { name: d, exact: true }),
		).toHaveAttribute("aria-pressed", "true");
	await expect(editor.getByLabel("Timezone")).toHaveValue(TIMEZONE);
	await expect(
		editor.getByLabel("If the last run is still going"),
	).toHaveValue("skip");
	await expect(
		editor.getByLabel("Runs missed while asleep or stopped"),
	).toHaveValue("run_once");
	await expect(editor.getByTestId("trigger-preview").locator("li")).toHaveCount(
		3,
	);
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

