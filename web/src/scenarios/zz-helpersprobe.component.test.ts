import { test } from "vitest";
import { page } from "vitest/browser";
test("probe", async () => {
	let p = Object.getPrototypeOf(page.getByTestId("a"));
	while (p) {
		const d = Object.getOwnPropertyDescriptor(p, "locator");
		console.log("PROTO", p.constructor?.name, d ? String(d.value).slice(0, 300) : "-");
		p = Object.getPrototypeOf(p);
	}
});
