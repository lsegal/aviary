import type { Locator } from "@playwright/test";

/** The element's bounding box; fails the test when it isn't rendered. */
export async function boundingBoxOf(locator: Locator) {
	const box = await locator.boundingBox();
	if (!box) throw new Error("element has no bounding box");
	return box;
}
