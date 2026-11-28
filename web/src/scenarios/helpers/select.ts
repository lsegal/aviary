// Mirrors web/e2e/helpers/select.ts for component tests (#1636).
import { expect } from "vitest";
import { commands, type Locator, page } from "vitest/browser";

// Drives the shared AppSelect/AppCombobox (web/src/components/ui/select).

/** The open dropdown's listbox. */
export function listbox(_field?: Locator): Locator {
	return page.getByRole("listbox");
}

async function expectClosed() {
	await expect.poll(() => listbox().elements().length).toBe(0);
}

/** Opens field and picks the option labelled name. */
export async function chooseOption(field: Locator, name: string | RegExp) {
	await field.click();
	await listbox(field)
		.getByRole("option", { name, exact: typeof name === "string" })
		.click();
	await expectClosed();
}

/** Opens field and picks the option whose value is value. */
export async function chooseValue(field: Locator, value: string) {
	await field.click();
	await listbox(field)
		.locator(`[role="option"][data-value="${value}"]`)
		.click();
	await expectClosed();
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
	await expectClosed();
}

/** Opens field, returns its option labels in order, and closes it again. */
export async function optionLabels(field: Locator): Promise<string[]> {
	await field.click();
	const options = listbox(field).getByRole("option");
	await expect.element(options.first()).toBeVisible();
	const labels = options
		.elements()
		.map(
			(el) =>
				(el.querySelector("[data-option-label]") ?? el).textContent?.trim() ??
				"",
		);
	await commands.keyPress("Escape");
	await expectClosed();
	return labels;
}

/** Expects field to hold value (the option value, not its label). */
export async function expectValue(field: Locator, value: string) {
	await expect.element(field).toHaveAttribute("data-value", value);
}
