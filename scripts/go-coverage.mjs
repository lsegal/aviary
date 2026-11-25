// Summarizes a `go test -coverprofile` profile as a total and per-package
// statement coverage report (#1217).
import { appendFile, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

// The packages listed under "Lowest coverage" in the summary.
const LOWEST_COUNT = 10;

// Returns per-package statement totals from a coverage profile. A block can
// appear more than once when profiles are merged, so it counts as covered
// when any of its entries ran.
export function parseProfile(text) {
	const blocks = new Map();
	for (const line of text.split(/\r?\n/)) {
		if (!line || line.startsWith("mode:")) continue;
		const match = line.match(/^(.+):(\d+\.\d+,\d+\.\d+) (\d+) (\d+)$/);
		if (!match) throw new Error(`Unrecognized coverage profile line: ${line}`);
		const [, file, range, statements, count] = match;
		const key = `${file}:${range}`;
		const block = blocks.get(key);
		if (block) {
			block.covered ||= count !== "0";
		} else {
			blocks.set(key, {
				pkg: path.posix.dirname(file),
				statements: Number(statements),
				covered: count !== "0",
			});
		}
	}
	const packages = new Map();
	for (const { pkg, statements, covered } of blocks.values()) {
		const totals = packages.get(pkg) ?? { statements: 0, covered: 0 };
		totals.statements += statements;
		if (covered) totals.covered += statements;
		packages.set(pkg, totals);
	}
	return packages;
}

export function percent({ statements, covered }) {
	return statements === 0 ? 0 : (covered / statements) * 100;
}

// Builds the Markdown report: the total, the lowest-coverage packages, and
// every package sorted by import path. Packages without statements are left
// out because they have nothing to cover.
export function formatReport(packages, modulePath) {
	const rows = [...packages]
		.filter(([, totals]) => totals.statements > 0)
		.map(([pkg, totals]) => ({
			pkg: modulePath && pkg.startsWith(`${modulePath}/`)
				? pkg.slice(modulePath.length + 1)
				: pkg === modulePath
					? "."
					: pkg,
			...totals,
		}));
	const total = rows.reduce(
		(sum, row) => ({
			statements: sum.statements + row.statements,
			covered: sum.covered + row.covered,
		}),
		{ statements: 0, covered: 0 },
	);
	const table = (list) => [
		"| Package | Coverage | Statements |",
		"| --- | ---: | ---: |",
		...list.map(
			(row) =>
				`| \`${row.pkg}\` | ${percent(row).toFixed(1)}% | ${row.covered} / ${row.statements} |`,
		),
	];
	const lowest = [...rows]
		.sort((a, b) => percent(a) - percent(b) || b.statements - a.statements)
		.slice(0, LOWEST_COUNT);
	const byPath = [...rows].sort((a, b) => a.pkg.localeCompare(b.pkg));
	return {
		total: percent(total),
		markdown: [
			"## Go coverage",
			"",
			`Total: **${percent(total).toFixed(1)}%** of statements (${total.covered} / ${total.statements}) across ${rows.length} packages.`,
			"",
			"### Lowest coverage",
			"",
			...table(lowest),
			"",
			"<details>",
			"<summary>All packages</summary>",
			"",
			...table(byPath),
			"",
			"</details>",
			"",
		].join("\n"),
	};
}

async function readModulePath(rootDir) {
	const goMod = await readFile(path.join(rootDir, "go.mod"), "utf8");
	return goMod.match(/^module\s+(\S+)/m)?.[1] ?? "";
}

async function main(argv) {
	const [profilePath, reportPath] = argv;
	if (!profilePath) {
		throw new Error(
			"Usage: node scripts/go-coverage.mjs <coverprofile> [report.md]",
		);
	}
	const rootDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
	const packages = parseProfile(await readFile(profilePath, "utf8"));
	const { total, markdown } = formatReport(packages, await readModulePath(rootDir));
	if (reportPath) await writeFile(reportPath, markdown, "utf8");
	if (process.env.GITHUB_STEP_SUMMARY) {
		await appendFile(process.env.GITHUB_STEP_SUMMARY, markdown, "utf8");
	}
	console.log(markdown);
	console.log(`Go coverage: ${total.toFixed(1)}%`);
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
	await main(process.argv.slice(2));
}
