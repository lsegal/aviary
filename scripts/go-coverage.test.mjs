import assert from "node:assert/strict";
import { test } from "node:test";
import { formatReport, parseProfile } from "./go-coverage.mjs";

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
