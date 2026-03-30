import { appendFile, mkdir, writeFile } from "node:fs/promises";
import { execFileSync } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";

const rootDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

function parseArgs(argv) {
	const args = {};
	for (let i = 0; i < argv.length; i += 1) {
		const arg = argv[i];
		if (!arg.startsWith("--")) continue;
		const key = arg.slice(2);
		const next = argv[i + 1];
		if (!next || next.startsWith("--")) {
			args[key] = "true";
			continue;
		}
		args[key] = next;
		i += 1;
	}
	return args;
}

function readCoverage(coverprofilePath) {
	const output = execFileSync("go", ["tool", "cover", "-func", coverprofilePath], {
		cwd: rootDir,
		encoding: "utf8",
		stdio: ["ignore", "pipe", "inherit"],
	});
	const match = output.match(/^total:\s+\(statements\)\s+([\d.]+)%$/m);
	if (!match) {
		throw new Error(`Unable to parse total coverage from ${coverprofilePath}`);
	}
	return Number.parseFloat(match[1]);
}

function formatCoverage(value) {
	return `${value.toFixed(1)}%`;
}

function badgeColor(coverage) {
	if (coverage >= 80) return "brightgreen";
	if (coverage >= 70) return "green";
	if (coverage >= 60) return "yellowgreen";
	if (coverage >= 50) return "yellow";
	if (coverage >= 40) return "orange";
	return "red";
}

async function writeGithubOutput(outputPath, values) {
	if (!outputPath) return;
	const lines = Object.entries(values).map(([key, value]) => `${key}=${value}`);
	await appendFile(outputPath, `${lines.join("\n")}\n`, "utf8");
}

async function writeBadge(badgePath, coverage) {
	if (!badgePath) return;
	await mkdir(path.dirname(badgePath), { recursive: true });
	await writeFile(
		badgePath,
		JSON.stringify(
			{
				schemaVersion: 1,
				label: "go coverage",
				message: formatCoverage(coverage),
				color: badgeColor(coverage),
			},
			null,
			2,
		),
		"utf8",
	);
}

async function writeSummary(summaryPath, current, base, threshold) {
	if (!summaryPath) return;
	const lines = ["## Go coverage", "", `Current: ${formatCoverage(current)}`];
	if (typeof base === "number") {
		const delta = current - base;
		lines.push(`Base: ${formatCoverage(base)}`);
		lines.push(`Delta: ${delta >= 0 ? "+" : ""}${delta.toFixed(1)} percentage points`);
		lines.push(`Allowed drop: ${threshold.toFixed(1)} percentage points`);
	}
	lines.push("");
	await appendFile(summaryPath, `${lines.join("\n")}\n`, "utf8");
}

const args = parseArgs(process.argv.slice(2));

if (!args.current) {
	throw new Error("Missing required --current <coverprofile> argument");
}

const current = readCoverage(args.current);
const base = args.base ? readCoverage(args.base) : null;
const threshold = Number.parseFloat(args.threshold ?? "5");
const delta = typeof base === "number" ? current - base : null;

await writeBadge(args.badge, current);
await writeGithubOutput(args["github-output"] ?? process.env.GITHUB_OUTPUT, {
	current: current.toFixed(1),
	current_display: formatCoverage(current),
	...(typeof base === "number"
		? {
				base: base.toFixed(1),
				base_display: formatCoverage(base),
				delta: delta.toFixed(1),
		  }
		: {}),
});
await writeSummary(args.summary ?? process.env.GITHUB_STEP_SUMMARY, current, base, threshold);

if (typeof base === "number" && delta < -threshold) {
	const drop = Math.abs(delta);
	console.error(
		`Go coverage dropped from ${formatCoverage(base)} to ${formatCoverage(current)} (${drop.toFixed(1)} percentage points), which exceeds the ${threshold.toFixed(1)} point limit.`,
	);
	process.exit(1);
}

console.log(`Go coverage: ${formatCoverage(current)}`);
if (typeof base === "number") {
	console.log(`Base coverage: ${formatCoverage(base)}`);
	console.log(`Delta: ${delta >= 0 ? "+" : ""}${delta.toFixed(1)} percentage points`);
}
