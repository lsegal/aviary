// Component-test helper, ported from the Playwright helper of the same name (#1636).
import { expect } from "vitest";
import type { Locator } from "vitest/browser";

/** The element's bounding box; fails the test when it isn't rendered. */
export async function boundingBoxOf(locator: Locator) {
	// Playwright's boundingBox waits for the element to attach.
	await expect.poll(() => locator.query()).toBeTruthy();
	const el = locator.element();
	if (el.getClientRects().length === 0)
		throw new Error("element has no bounding box");
	return el.getBoundingClientRect();
}
