import { globSync, readFileSync } from "node:fs";
import path from "node:path";
import tailwindcss from "@tailwindcss/vite";
import vue from "@vitejs/plugin-vue";
import { playwright } from "@vitest/browser-playwright";
import type { Plugin } from "vite";
import { configDefaults, defineConfig } from "vitest/config";
import {
	emulateMedia,
	keyPress,
	keyType,
	mouseDown,
	mouseMove,
	mouseUp,
	mouseWheel,
} from "./web/src/component-test/commands.ts";

// Component tests (`pnpm test:component`, #1636): Vitest browser mode mounts
// the web app in Chromium against a fake server, with its real stylesheet.
// They carry the web scenario coverage; Playwright (web/e2e) keeps only what
// needs the built app and the real server, and the happy-dom unit tests
// (vitest.config.ts) cover modules on their own. See ARCHITECTURE.md
// (Testing).

// What every component test depends on beyond its own imports: the setup
// file, the harness and the global stylesheet. `vitest --changed` follows
// only each test's imports, so a change to any of these reruns them all.
const GLOBAL_TRIGGERS = [
	"**/web/src/component-test/**",
	"**/web/src/style.css",
	"**/web/e2e/helpers/toolArguments.json",
	"**/vitest.component.config.ts",
	"**/pnpm-lock.yaml",
];

// Every package the app imports, prebundled before the first test. Vite
// otherwise discovers them as lazy routes load and stalls the run while it
// re-optimizes, long enough to time a test out.
const PREBUNDLED = [
	...new Set(
		globSync("web/src/**/*.{ts,vue}", {
			exclude: (file) => file.endsWith(".test.ts"),
		}).flatMap((file) =>
			[...readFileSync(file, "utf8").matchAll(/\bfrom\s+"([^"./][^"]*)"/g)]
				.map((m) => m[1])
				.filter((name) => !name.startsWith("vitest")),
		),
	),
].sort();

// The router's lazy page imports, hidden from the module graph so a test
// depends only on the pages it imports itself, not on every page the router
// can load; see web/src/component-test/routes.ts. Every .vue module also
// notes that it evaluated, which is how routes.ts tells a declared page from
// an undeclared one.
const ROUTER_FILES = /\/web\/src\/router\/(index|settingsView)\.ts$/;
function routePagesAsDeclared(): Plugin {
	let root = process.cwd();
	return {
		name: "aviary:route-pages-as-declared",
		enforce: "post",
		configResolved(config) {
			root = config.root;
		},
		transform(code, id) {
			const file = id.split("?")[0];
			if (ROUTER_FILES.test(file)) {
				return code.replace(
					/\bimport\(\s*(["'])(\.{1,2}\/[^"']+\.vue)\1\s*\)/g,
					(_, _quote, spec) => {
						const target = path.resolve(path.dirname(file), spec);
						const url = `/${path.relative(root, target)}`;
						return `globalThis.__aviaryRoute(${JSON.stringify(target)}, ${JSON.stringify(url)})`;
					},
				);
			}
			if (file.endsWith(".vue") && !id.includes("?")) {
				return `${code}\n(globalThis.__aviaryEvaluated ??= new Set()).add(${JSON.stringify(file)});\n`;
			}
		},
	};
}

export default defineConfig({
	plugins: [vue(), tailwindcss(), routePagesAsDeclared()],
	define: {
		"import.meta.env.COMPONENT_TEST_RECORD_ROUTES": JSON.stringify(
			process.env.COMPONENT_TEST_RECORD_ROUTES ?? "",
		),
		"import.meta.env.COMPONENT_TEST_ROUTES_DIR": JSON.stringify(
			`${import.meta.dirname}/.vitest/routes`,
		),
	},
	server: {
		// Vitest picks the browser server's port and moves past one in use.
		strictPort: false,
		// `vitest --changed` walks the tests' imports through the server's
		// ssr environment, whose CSS plugins only initialize when each
		// environment runs its own buildStart.
		perEnvironmentStartEndDuringDev: true,
		// Transforms the app up front: lazy routes otherwise pay for it the
		// first time a test navigates to them, long enough to time it out.
		warmup: { clientFiles: ["web/src/**/*.vue"] },
	},
	optimizeDeps: { include: PREBUNDLED },
	test: {
		include: ["web/src/**/*.component.test.ts"],
		setupFiles: ["web/src/component-test/setup.ts"],
		forceRerunTriggers: [
			...configDefaults.forceRerunTriggers,
			...GLOBAL_TRIGGERS,
		],
		css: { include: /.+/ },
		// Playwright waits up to 5 s for an expectation; Vitest's 1 s poll
		// gives up too soon on a busy runner.
		expect: { poll: { timeout: 5_000 } },
		// Playwright's per-test timeout and CI retries, which the scenarios
		// had as specs: a busy runner can slow a long round trip past it.
		testTimeout: 30_000,
		retry: process.env.CI ? 2 : 0,
		browser: {
			enabled: true,
			headless: true,
			provider: playwright(),
			instances: [{ browser: "chromium" }],
			commands: {
				emulateMedia,
				keyPress,
				keyType,
				mouseDown,
				mouseMove,
				mouseUp,
				mouseWheel,
			},
			viewport: { width: 1280, height: 720 },
		},
	},
});
