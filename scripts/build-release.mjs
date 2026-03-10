import { createHash } from "node:crypto";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { execFileSync } from "node:child_process";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const rootDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const releaseDir = path.join(rootDir, "dist", "release");
const defaultTargets = [
	{ goos: "windows", goarch: "amd64" },
	{ goos: "linux", goarch: "amd64" },
	{ goos: "linux", goarch: "arm64" },
	{ goos: "darwin", goarch: "amd64" },
	{ goos: "darwin", goarch: "arm64" },
];

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

function parseTargets(raw) {
	if (!raw) return defaultTargets;
	return raw.split(",").map((entry) => {
		const [goos, goarch] = entry.trim().split("/");
		if (!goos || !goarch) {
			throw new Error(`invalid target ${entry}; expected os/arch`);
		}
		return { goos, goarch };
	});
}

function assetBaseName(version, goos, goarch) {
	return `aviary_${version}_${goos}_${goarch}`;
}

function binaryName(goos) {
	return goos === "windows" ? "aviary.exe" : "aviary";
}

const args = parseArgs(process.argv.slice(2));
const version = resolveVersion(args.version);
const targets = parseTargets(args.targets);
const checksums = [];

await rm(releaseDir, { recursive: true, force: true });
await mkdir(releaseDir, { recursive: true });

for (const target of targets) {
	const baseName = assetBaseName(version, target.goos, target.goarch);
	const stageDir = await mkdtemp(path.join(tmpdir(), `${baseName}-`));
	const binaryPath = path.join(stageDir, binaryName(target.goos));
	const archivePath = path.join(releaseDir, `${baseName}.tar.gz`);

	try {
		execFileSync(
			"node",
			[
				path.join("scripts", "build-go.mjs"),
				"--version",
				version,
				"--goos",
				target.goos,
				"--goarch",
				target.goarch,
				"--output",
				binaryPath,
			],
			{
				cwd: rootDir,
				stdio: "inherit",
			},
		);

		execFileSync(
			"tar",
			["-czf", archivePath, "-C", stageDir, binaryName(target.goos)],
			{
				cwd: rootDir,
				stdio: "inherit",
			},
		);

		const sha256 = createHash("sha256")
			.update(await readFile(archivePath))
			.digest("hex");
		checksums.push(`${sha256}  ${path.basename(archivePath)}`);
	} finally {
		await rm(stageDir, { recursive: true, force: true });
	}
}

await writeFile(path.join(releaseDir, "checksums.txt"), `${checksums.join("\n")}\n`);
