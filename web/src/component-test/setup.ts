// Setup for every component test (#1636): the app's stylesheet, the fake
// server, Playwright-style locators and matchers, and a clean page after
// each test. See ARCHITECTURE.md (Testing).

import "../style.css";
import { afterEach, beforeEach, expect } from "vitest";
import { commands, locators, page } from "vitest/browser";
import { settleAndUnmount } from "./app";
// Before ./app: the router it loads sends its page imports to routes.ts.
import { writeRecordedRoutes } from "./routes";
import { installServer, resetServer } from "./server";

declare module "vitest/browser" {
	interface LocatorSelectors {
		/** A Playwright selector, as page.locator takes: CSS by default. */
		locator(selector: string): import("vitest/browser").Locator;
	}
}

// Vitest's locators build every chained query (loc.getByRole(), .nth(),
// .filter()) through their own prototype's `locator(selector)`, which
// locators.extend would replace with a wrapper that calls it again. Keep the
// prototype's own method and extend only `page`, where chaining off a
// locator then reaches the original.
const locatorProto = Object.getPrototypeOf(page.getByTestId("probe")) as {
	locator: unknown;
};
const ownLocator = locatorProto.locator;
locators.extend({
	locator(selector: string) {
		return selector;
	},
});
locatorProto.locator = ownLocator;

// Playwright matches text, names, labels and placeholders as
// case-insensitive substrings unless told `exact: true`; Vitest matches
// exactly by default. Default them as Playwright does, so a scenario finds
// the same elements it did as a spec.
const TEXT_LOCATORS = [
	"getByText",
	"getByRole",
	"getByLabelText",
	"getByPlaceholder",
	"getByTitle",
	"getByAltText",
] as const;
type Patchable = Record<string, (...args: unknown[]) => unknown>;
function inexactByDefault(target: Patchable) {
	for (const name of TEXT_LOCATORS) {
		const original = target[name];
		if (
			typeof original !== "function" ||
			(original as { inexact?: true }).inexact
		)
			continue;
		const patched = function (this: unknown, ...args: unknown[]) {
			const options = (args[1] ?? {}) as { exact?: boolean };
			args[1] = { ...options, exact: options.exact ?? false };
			return original.apply(this, args);
		};
		(patched as { inexact?: true }).inexact = true;
		target[name] = patched;
	}
}
inexactByDefault(page as unknown as Patchable);
inexactByDefault(
	Object.getPrototypeOf(page.getByTestId("probe")) as unknown as Patchable,
);

function normalize(text: string): string {
	return text.replace(/\s+/g, " ").trim();
}

declare module "vitest" {
	interface Matchers<
		R extends void | Promise<void> = void | Promise<void>,
		T = unknown,
	> {
		/** The element's whole text, whitespace collapsed, equals `expected`
		 * (or matches it), as Playwright's toHaveText. */
		toHaveText(expected: string | RegExp): R;
		/** The element's computed `property` equals `expected`. */
		toHaveCSS(property: string, expected: string | RegExp): R;
	}
}

expect.extend({
	toHaveText(received: Element, expected: string | RegExp) {
		const actual = normalize(received?.textContent ?? "");
		const pass =
			typeof expected === "string"
				? actual === normalize(expected)
				: expected.test(actual);
		return {
			pass,
			message: () =>
				`expected text ${JSON.stringify(actual)} ${pass ? "not " : ""}to ${
					typeof expected === "string" ? "equal" : "match"
				} ${String(expected)}`,
		};
	},
	toHaveCSS(received: Element, property: string, expected: string | RegExp) {
		const actual = getComputedStyle(received).getPropertyValue(property);
		const pass =
			typeof expected === "string"
				? actual === expected
				: expected.test(actual);
		return {
			pass,
			message: () =>
				`expected ${property} ${JSON.stringify(actual)} ${pass ? "not " : ""}to be ${String(expected)}`,
		};
	},
});

installServer();

beforeEach(async () => {
	localStorage.clear();
	sessionStorage.clear();
	await page.viewport(1280, 720);
});

afterEach(async () => {
	await writeRecordedRoutes(expect.getState().testPath ?? "");
	await settleAndUnmount();
	resetServer();
	// Dialogs and toasts teleport to the body; drop whatever outlived the app.
	document.body.replaceChildren();
	localStorage.clear();
	sessionStorage.clear();
	await commands.emulateMedia({ colorScheme: null, reducedMotion: null });
});
