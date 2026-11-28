import { expect, test } from "vitest";
import { page } from "vitest/browser";
import { goto, renderApp } from "../component-test/app";
import {
	expectNoHorizontalOverflow,
	mockSettings,
} from "./settings-mobile-overflow.fixtures";
// The pages this file renders (see component-test/routes.ts).
import "../views/SettingsView.vue";

// Settings pages fit a phone: nothing widens the page or the settings
// scroller past the screen, so the header never slides off the left edge
// (#997). A fieldset's default min-inline-size used to let one long preview
// line widen the whole workflow form. The largest text size is in
// settings-mobile-overflow-text-size.

for (const width of [320, 375, 430]) {
	test(`settings pages fit a ${width}px phone`, async () => {
		await page.viewport(width, 800);
		mockSettings();

		let first = true;
		for (const url of [
			"/agents/Aviary/channels/signal/settings",
			"/agents/Aviary/tasks?task=release-aviary",
			"/agents/Aviary/settings",
			"/projects/settings?agent=Aviary&project=o%2Fr",
			"/projects/settings",
			"/system/settings",
			"/system/display",
		]) {
			if (first) await renderApp(url);
			else await goto(url);
			first = false;
			await expectNoHorizontalOverflow(url);
		}

		// The workflow form with its trigger section, a long preview and the
		// takeover warning showing.
		await goto("/agents/Aviary/channels/signal/settings?tab=workflows");
		await page.getByTestId("workflow-add").click();
		await page.getByTestId("workflow-match").fill("*");
		await expect
			.element(page.getByTestId("workflow-takeover-hint"))
			.toBeVisible();
		await expect
			.poll(
				() => page.getByTestId("workflow-preview-message").elements().length,
			)
			.toBe(2);
		await expectNoHorizontalOverflow("workflow form");
		const scroller = page
			.getByTestId("settings-scroll")
			.element()
			.getBoundingClientRect();
		const hint = page
			.getByTestId("workflow-takeover-hint")
			.element()
			.getBoundingClientRect();
		expect(hint.x + hint.width).toBeLessThanOrEqual(
			scroller.x + scroller.width,
		);
	});
}
