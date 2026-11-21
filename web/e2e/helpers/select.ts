import { expect, type Locator } from "@playwright/test";

// Drives the shared AppSelect/AppCombobox (web/src/components/ui/select).

/** The open dropdown's listbox. */
export function listbox(field: Locator): Locator {
	return field.page().getByRole("listbox");
}

/** Opens field and picks the option labelled name. */
export async function chooseOption(field: Locator, name: string | RegExp) {
	await field.click();
	await listbox(field)
		.getByRole("option", { name, exact: typeof name === "string" })
		.click();
	await expect(listbox(field)).toHaveCount(0);
}

/** Opens field and picks the option whose value is value. */
export async function chooseValue(field: Locator, value: string) {
	await field.click();
	await listbox(field)
		.locator(`[role="option"][data-value="${value}"]`)
		.click();
	await expect(listbox(field)).toHaveCount(0);
}

/** Types term into a combobox's search input and picks the option labelled name. */
export async function searchOption(
	input: Locator,
	term: string,
	name: string | RegExp = term,
) {
	await input.click();
	await input.fill(term);
	await listbox(input)
		.getByRole("option", { name, exact: typeof name === "string" })
		.click();
	await expect(listbox(input)).toHaveCount(0);
}

/** Opens field, returns its option labels in order, and closes it again. */
export async function optionLabels(field: Locator): Promise<string[]> {
	await field.click();
	const options = listbox(field).getByRole("option");
	await expect(options.first()).toBeVisible();
	const labels = await options.evaluateAll((els) =>
		els.map(
			(el) =>
				(el.querySelector("[data-option-label]") ?? el).textContent?.trim() ??
				"",
		),
	);
	await field.page().keyboard.press("Escape");
	await expect(listbox(field)).toHaveCount(0);
	return labels;
}

/** Expects field to hold value (the option value, not its label). */
export async function expectValue(field: Locator, value: string) {
	await expect(field).toHaveAttribute("data-value", value);
}
