import { mkdir } from "node:fs/promises";
import { execFileSync } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { parseArgs } from "./lib/args.mjs";

const rootDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

function readGoEnv(key) {
	return execFileSync("go", ["env", key], {
		cwd: rootDir,
		encoding: "utf8",
	}).trim();
}

function resolveVersion(explicitVersion) {
	if (explicitVersion) return explicitVersion;
	if (process.env.AVIARY_VERSION) return process.env.AVIARY_VERSION;
	try {
		return execFileSync("git", ["describe", "--tags", "--always", "--dirty"], {
			cwd: rootDir,
			encoding: "utf8",
			stdio: ["ignore", "pipe", "ignore"],
		}).trim();
	} catch {
		return "dev";
	}
}

const args = parseArgs(process.argv.slice(2));
const goos = args.goos ?? process.env.GOOS ?? readGoEnv("GOOS");
const goarch = args.goarch ?? process.env.GOARCH ?? readGoEnv("GOARCH");
const version = resolveVersion(args.version);
const ext = goos === "windows" ? ".exe" : "";
const outputName = args.output ?? `aviary${ext}`;
const outputPath = path.isAbsolute(outputName)
	? outputName
	: path.join(rootDir, outputName);
const outputDir = path.dirname(outputPath);

await mkdir(outputDir, { recursive: true });

const env = {
	...process.env,
	GOOS: goos,
	GOARCH: goarch,
	CGO_ENABLED: args.cgo ?? "0",
};

execFileSync(
	"go",
	[
		"build",
		"-trimpath",
		"-ldflags",
		`-s -w -X github.com/lsegal/aviary/internal/buildinfo.Version=${version}`,
		"-o",
		outputPath,
		"./cmd/aviary",
	],
	{
		cwd: rootDir,
		env,
		stdio: "inherit",
	},
);
