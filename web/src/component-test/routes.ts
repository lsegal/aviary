// Route pages a component test may render (#1636).
//
// The router loads every page lazily, so in the module graph each test that
// mounts the app would depend on every page, and `vitest --changed` would
// select every test for any page change. The component-test config's
// routePagesAsDeclared plugin hides the router's lazy imports from the graph
// and sends them here instead. A test file declares the pages it renders,
// which puts exactly those pages in its graph:
//
//   declarePages(() => import("../views/DaemonsView.vue"));
//
// The import stays lazy, so a page still loads only when a test navigates
// to it, after the test has set up storage and fixtures. Rendering a page
// the file didn't declare fails the navigation, so a test can never depend
// on a page its selection doesn't know about.

import { commands } from "vitest/browser";

declare global {
	var __aviaryDeclared: Set<string> | undefined;
	var __aviaryRoute:
		| ((file: string, url: string) => Promise<unknown>)
		| undefined;
}

// Filled from each test file's declarePages calls by the plugin.
globalThis.__aviaryDeclared ??= new Set<string>();
const declared = globalThis.__aviaryDeclared;

/** Declares the pages a test file renders. The component-test config reads
 * the imports from the source, so this does nothing at run time. */
export function declarePages(..._pages: Array<() => Promise<unknown>>): void {}

// With COMPONENT_TEST_RECORD_ROUTES set, undeclared pages load and are
// listed in .vitest/routes/ instead, to declare them in bulk.
const recording = import.meta.env.COMPONENT_TEST_RECORD_ROUTES === "1";
const recorded = new Set<string>();

/** The import a scenario under web/src/scenarios writes for `path`. */
function importPath(path: string): string {
	return path.replace(/^.*?\/(views|components)\//, "../$1/");
}

globalThis.__aviaryRoute = (path: string, url: string) => {
	if (!declared.has(path)) {
		if (recording) {
			recorded.add(path);
		} else {
			const error = new Error(
				`This test renders ${path} without declaring it. Add \`declarePages(() => import("${importPath(path)}"))\` to the test file, so changes to that page select the test.`,
			);
			console.error(error.message);
			return Promise.reject(error);
		}
	}
	return import(/* @vite-ignore */ url);
};

/** Writes the pages this file rendered without declaring them (record mode). */
export async function writeRecordedRoutes(testFile: string): Promise<void> {
	if (!recording || !recorded.size) return;
	const name = testFile.replace(/[^\w.-]+/g, "_");
	await commands.writeFile(
		`${import.meta.env.COMPONENT_TEST_ROUTES_DIR}/${name}.json`,
		JSON.stringify({ file: testFile, pages: [...recorded].sort() }),
	);
}
