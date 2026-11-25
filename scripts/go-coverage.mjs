// Summarizes a `go test -coverprofile` profile as a total and per-package
// statement coverage report (#1217), and converts it to the Cobertura XML
// that GitHub's code coverage upload accepts (#1308).
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

// Returns per-file line hit counts from a coverage profile. A line inside
// several blocks, or inside a block repeated by merged profiles, keeps its
// highest count.
export function parseLines(text, modulePath) {
	const files = new Map();
	for (const line of text.split(/\r?\n/)) {
		if (!line || line.startsWith("mode:")) continue;
		const match = line.match(/^(.+):(\d+)\.\d+,(\d+)\.\d+ \d+ (\d+)$/);
		if (!match) throw new Error(`Unrecognized coverage profile line: ${line}`);
		const [, importPath, start, end, count] = match;
		const file =
			modulePath && importPath.startsWith(`${modulePath}/`)
				? importPath.slice(modulePath.length + 1)
				: importPath;
		const lines = files.get(file) ?? new Map();
		for (let n = Number(start); n <= Number(end); n++) {
			lines.set(n, Math.max(lines.get(n) ?? 0, Number(count)));
		}
		files.set(file, lines);
	}
	return files;
}

function escapeXML(value) {
	return value.replace(/[<>&"']/g, (c) => `&#${c.charCodeAt(0)};`);
}

function lineRate(covered, valid) {
	return valid === 0 ? "1" : (covered / valid).toFixed(4);
}

// Builds a Cobertura XML report from parseLines output, with one class per
// file and one package per directory. File names are relative to the
// repository root so GitHub can match them to the pull request's files.
export function formatCobertura(files, timestamp = Date.now()) {
	const packages = new Map();
	for (const [file, lines] of [...files].sort(([a], [b]) => a.localeCompare(b))) {
		const dir = path.posix.dirname(file);
		const list = packages.get(dir) ?? [];
		list.push({ file, lines: [...lines].sort(([a], [b]) => a - b) });
		packages.set(dir, list);
	}
	let totalValid = 0;
	let totalCovered = 0;
	const packageXML = [];
	for (const [dir, classes] of packages) {
		let pkgValid = 0;
		let pkgCovered = 0;
		const classXML = classes.map(({ file, lines }) => {
			const covered = lines.filter(([, hits]) => hits > 0).length;
			pkgValid += lines.length;
			pkgCovered += covered;
			return [
				`\t\t\t\t<class name="${escapeXML(path.posix.basename(file))}" filename="${escapeXML(file)}" line-rate="${lineRate(covered, lines.length)}" branch-rate="0" complexity="0">`,
				"\t\t\t\t\t<methods/>",
				"\t\t\t\t\t<lines>",
				...lines.map(
					([number, hits]) =>
						`\t\t\t\t\t\t<line number="${number}" hits="${hits}"/>`,
				),
				"\t\t\t\t\t</lines>",
				"\t\t\t\t</class>",
			].join("\n");
		});
		totalValid += pkgValid;
		totalCovered += pkgCovered;
		packageXML.push(
			[
				`\t\t<package name="${escapeXML(dir)}" line-rate="${lineRate(pkgCovered, pkgValid)}" branch-rate="0" complexity="0">`,
				"\t\t\t<classes>",
				...classXML,
				"\t\t\t</classes>",
				"\t\t</package>",
			].join("\n"),
		);
	}
	return {
		lineRate: totalValid === 0 ? 0 : (totalCovered / totalValid) * 100,
		xml: [
			'<?xml version="1.0" ?>',
			'<!DOCTYPE coverage SYSTEM "http://cobertura.sourceforge.net/xml/coverage-04.dtd">',
			`<coverage line-rate="${lineRate(totalCovered, totalValid)}" branch-rate="0" lines-covered="${totalCovered}" lines-valid="${totalValid}" branches-covered="0" branches-valid="0" complexity="0" version="1" timestamp="${Math.floor(timestamp / 1000)}">`,
			"\t<sources>",
			"\t\t<source>.</source>",
			"\t</sources>",
			"\t<packages>",
			...packageXML,
			"\t</packages>",
			"</coverage>",
			"",
		].join("\n"),
	};
}

async function readModulePath(rootDir) {
	const goMod = await readFile(path.join(rootDir, "go.mod"), "utf8");
	return goMod.match(/^module\s+(\S+)/m)?.[1] ?? "";
}

async function main(argv) {
	const coberturaIndex = argv.indexOf("--cobertura");
	const coberturaPath = coberturaIndex === -1 ? "" : argv[coberturaIndex + 1];
	if (coberturaIndex !== -1) argv.splice(coberturaIndex, 2);
	const [profilePath, reportPath] = argv;
	if (!profilePath || (coberturaIndex !== -1 && !coberturaPath)) {
		throw new Error(
			"Usage: node scripts/go-coverage.mjs <coverprofile> [report.md] [--cobertura report.xml]",
		);
	}
	const rootDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
	const modulePath = await readModulePath(rootDir);
	const profile = await readFile(profilePath, "utf8");
	const packages = parseProfile(profile);
	const { total, markdown } = formatReport(packages, modulePath);
	if (reportPath) await writeFile(reportPath, markdown, "utf8");
	if (coberturaPath) {
		const { lineRate, xml } = formatCobertura(parseLines(profile, modulePath));
		await writeFile(coberturaPath, xml, "utf8");
		console.log(`Go line coverage: ${lineRate.toFixed(1)}%`);
	}
	if (process.env.GITHUB_STEP_SUMMARY) {
		await appendFile(process.env.GITHUB_STEP_SUMMARY, markdown, "utf8");
	}
	console.log(markdown);
	console.log(`Go coverage: ${total.toFixed(1)}%`);
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
	await main(process.argv.slice(2));
}
