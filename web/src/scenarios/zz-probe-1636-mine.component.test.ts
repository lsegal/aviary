import { expect, test } from "vitest";
import { page } from "vitest/browser";
test("probe", () => {
	document.body.innerHTML = `<div class="grid"><div class="rounded-xl" data-testid="c" data-k="1">a<b data-testid="t">x</b></div><div class="rounded-xl" data-testid="c" data-k="2">b<b data-testid="t">y</b></div></div>`;
	const r: string[] = [];
	const l = page.locator(".grid > div.rounded-xl");
	const tries: Record<string, () => string> = {
		cssGetBy: () => String(l.getByTestId("t").elements().length),
		cssGetByText: () => String(l.getByText("y").elements().length),
		tidNth: () => page.getByTestId("c").nth(1).element().textContent ?? "",
		tidFilter: () => page.getByTestId("c").filter({ hasText: "b" }).element().textContent ?? "",
		elLoc: () => page.elementLocator(l.elements()[1]).getByTestId("t").element().textContent ?? "",
		attr: () => page.locator('[data-testid=c][data-k="2"]').getByTestId("t").element().textContent ?? "",
		and: () => page.getByTestId("c").and(page.locator('[data-k="2"]')).element().textContent ?? "",
	};
	for (const [k, f] of Object.entries(tries)) {
		try { r.push(`${k}=${f()}`); } catch (e) { r.push(`${k} ERR ${e}`); }
	}
	console.log(JSON.stringify(r));
	expect(1).toBe(1);
});
