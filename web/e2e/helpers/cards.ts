import type { Locator } from "@playwright/test";
import { boundingBoxOf } from "./box";

// Clicks a card where part of its body shows; the card's stretched link
// covers the body, so the click lands on the link as a person's would.
export async function clickCardAt(
	card: Locator,
	part: Locator,
	modifiers?: ("ControlOrMeta" | "Shift")[],
) {
	const c = await boundingBoxOf(card);
	const p = await boundingBoxOf(part);
	await card.click({
		position: { x: p.x - c.x + 4, y: p.y - c.y + p.height / 2 },
		modifiers,
	});
}
