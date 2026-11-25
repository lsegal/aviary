import assert from "node:assert/strict";
import { test } from "node:test";
import {
	formatCobertura,
	formatReport,
	parseLines,
	parseProfile,
} from "./go-coverage.mjs";

const profile = `mode: atomic
example.com/m/a/a.go:1.1,3.2 4 1
example.com/m/a/a.go:5.1,7.2 6 0
example.com/m/a/b.go:1.1,2.2 10 3
example.com/m/b/b.go:1.1,2.2 5 0
example.com/m/b/b.go:1.1,2.2 5 2
example.com/m/c/c.go:1.1,2.2 8 0
example.com/m/main.go:1.1,2.2 2 2
`;

test("totals statements per package and counts merged blocks once", () => {
	const packages = parseProfile(profile);
	assert.deepEqual(Object.fromEntries(packages), {
		"example.com/m/a": { statements: 20, covered: 14 },
		"example.com/m/b": { statements: 5, covered: 5 },
		"example.com/m/c": { statements: 8, covered: 0 },
		"example.com/m": { statements: 2, covered: 2 },
	});
});

test("rejects lines that are not coverage blocks", () => {
	assert.throws(() => parseProfile("mode: set\nnot a block\n"), /not a block/);
});

test("reports the total and lists the lowest-coverage packages first", () => {
	const { total, markdown } = formatReport(parseProfile(profile), "example.com/m");
	assert.equal(total, (21 / 35) * 100);
	assert.match(markdown, /Total: \*\*60\.0%\*\* of statements \(21 \/ 35\) across 4 packages\./);
	const lowest = markdown.split("### Lowest coverage")[1].split("<details>")[0];
	assert.deepEqual(
		[...lowest.matchAll(/^\| `([^`]+)`/gm)].map((m) => m[1]),
		["c", "a", "b", "."],
	);
	assert.match(markdown, /\| `a` \| 70\.0% \| 14 \/ 20 \|/);
});

test("maps blocks to repository-relative line hits, keeping the highest count", () => {
	const files = parseLines(
		`mode: atomic
example.com/m/a/a.go:1.1,3.2 4 0
example.com/m/a/a.go:3.5,4.2 1 2
example.com/m/a/a.go:1.1,3.2 4 1
other.org/x/x.go:2.1,2.9 1 0
`,
		"example.com/m",
	);
	assert.deepEqual(
		Object.fromEntries([...files].map(([file, lines]) => [file, Object.fromEntries(lines)])),
		{
			"a/a.go": { 1: 1, 2: 1, 3: 2, 4: 2 },
			"other.org/x/x.go": { 2: 0 },
		},
	);
});

test("writes Cobertura XML with per-file classes and line totals", () => {
	const files = parseLines(
		`mode: set
example.com/m/a/a.go:1.1,2.2 1 1
example.com/m/a/b.go:5.1,5.9 1 0
example.com/m/main.go:3.1,3.9 1 1
`,
		"example.com/m",
	);
	const { lineRate, xml } = formatCobertura(files, 1_700_000_000_000);
	assert.equal(lineRate, 75);
	assert.match(
		xml,
		/<coverage line-rate="0\.7500" branch-rate="0" lines-covered="3" lines-valid="4" [^>]*timestamp="1700000000">/,
	);
	assert.match(xml, /<package name="a" line-rate="0\.6667"/);
	assert.match(xml, /<class name="b\.go" filename="a\/b\.go" line-rate="0\.0000"[^>]*>\s*<methods\/>\s*<lines>\s*<line number="5" hits="0"\/>/);
	assert.match(xml, /<package name="\." line-rate="1\.0000"/);
	assert.match(xml, /<class name="main\.go" filename="main\.go"/);
});
