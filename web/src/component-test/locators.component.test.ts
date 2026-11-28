import { expect, test } from "vitest";
import { page } from "vitest/browser";

// The Playwright-style locators setup.ts adds: page.locator, chaining off
// any locator, and case-insensitive substring text matching by default.
test("page.locator takes CSS and chains like any locator", async () => {
	document.body.innerHTML =
		'<div data-testid="a"><p>one</p><p>two <code>x</code></p></div>';
	const a = page.getByTestId("a");
	expect(a.locator("p").elements()).toHaveLength(2);
	expect(a.locator("p").nth(1).locator("code").element().textContent).toBe("x");
	expect(a.getByText("TWO").elements()).toHaveLength(1);
	expect(
		page.locator("[data-testid=a] p").last().element().textContent,
	).toContain("two");
	expect(a.locator("p").filter({ hasText: "one" }).elements()).toHaveLength(1);
});
