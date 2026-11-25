// Collects browser JavaScript coverage from the E2E run and reports it as
// Cobertura XML for GitHub's code coverage upload (#1308). It is off unless
// AVIARY_E2E_COVERAGE is set, and needs a web build made with
// AVIARY_WEB_SOURCEMAP=1 so the bundle maps back to web/src.
import { readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import {
	CoverageReport,
	type CoverageReportOptions,
} from "monocart-coverage-reports";

const rootDir = path.resolve(
	path.dirname(fileURLToPath(import.meta.url)),
	"../../..",
);
const distDir = path.join(rootDir, "web", "dist");

export const coverageEnabled = !!process.env.AVIARY_E2E_COVERAGE;

const options: CoverageReportOptions = {
	name: "Web E2E coverage",
	outputDir: path.join(rootDir, "coverage", "web"),
	reports: [
		["cobertura", { file: "cobertura.xml", projectRoot: rootDir }],
		["markdown-summary", { outputFile: "coverage-summary.md" }],
		["console-summary"],
	],
	// Only the app's own bundle; the server also serves third-party scripts.
	entryFilter: (entry) => {
		const { pathname } = new URL(entry.url);
		return pathname.startsWith("/assets/") && pathname.endsWith(".js");
	},
	sourceFilter: (sourcePath) => sourcePath.startsWith("web/src/"),
	// Vite's maps name sources relative to the bundle in web/dist/assets.
	sourcePath: (filePath) => {
		const index = filePath.indexOf("src/");
		return index === -1 ? filePath : `web/${filePath.slice(index)}`;
	},
	// The maps are read from the local build, not fetched from the server.
	sourceMapResolver: async (url) => {
		const file = path.join(distDir, new URL(url).pathname);
		return JSON.parse(await readFile(file, "utf8"));
	},
};

export function coverageReport() {
	return new CoverageReport(options);
}
