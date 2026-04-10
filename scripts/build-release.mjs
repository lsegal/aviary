import { createHash } from "node:crypto";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { execFileSync } from "node:child_process";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const rootDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const releaseDir = path.join(rootDir, "dist", "release");
const scoopManifestPath = path.join(rootDir, "bucket", "aviary.json");
const homebrewFormulaPath = path.join(rootDir, "Formula", "aviary.rb");
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

function scoopVersion(version) {
	return version.startsWith("v") ? version.slice(1) : version;
}

function releaseVersion(version) {
	return version.startsWith("v") ? version.slice(1) : version;
}

function releaseAssetURL(version, goos, goarch) {
	return `https://github.com/lsegal/aviary/releases/download/${version}/${assetBaseName(version, goos, goarch)}.tar.gz`;
}

function renderHomebrewFormula(version, hashes) {
	const formulaVersion = releaseVersion(version);
	return `class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "${formulaVersion}"

  on_macos do
    on_arm do
      url "${releaseAssetURL(version, "darwin", "arm64")}"
      sha256 "${hashes.darwin_arm64}"
    end

    on_intel do
      url "${releaseAssetURL(version, "darwin", "amd64")}"
      sha256 "${hashes.darwin_amd64}"
    end
  end

  on_linux do
    on_arm do
      url "${releaseAssetURL(version, "linux", "arm64")}"
      sha256 "${hashes.linux_arm64}"
    end

    on_intel do
      url "${releaseAssetURL(version, "linux", "amd64")}"
      sha256 "${hashes.linux_amd64}"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
`;
}

const args = parseArgs(process.argv.slice(2));
const version = resolveVersion(args.version);
const targets = parseTargets(args.targets);
const checksums = [];
let windowsAmd64Hash = null;
const releaseHashes = {};

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
		releaseHashes[`${target.goos}_${target.goarch}`] = sha256;
		if (target.goos === "windows" && target.goarch === "amd64") {
			windowsAmd64Hash = sha256;
		}
	} finally {
		await rm(stageDir, { recursive: true, force: true });
	}
}

await writeFile(path.join(releaseDir, "checksums.txt"), `${checksums.join("\n")}\n`);

if (windowsAmd64Hash) {
	await mkdir(path.dirname(scoopManifestPath), { recursive: true });
	const manifest = {
		version: scoopVersion(version),
		description: "Aviary: the AI Agent Nest",
		homepage: "https://aviary.bot",
		license: "MIT",
		architecture: {
			"64bit": {
				url: `https://github.com/lsegal/aviary/releases/download/${version}/aviary_${version}_windows_amd64.tar.gz`,
				hash: windowsAmd64Hash,
			},
		},
		bin: "aviary.exe",
		checkver: {
			github: "https://github.com/lsegal/aviary",
		},
		autoupdate: {
			architecture: {
				"64bit": {
					url: "https://github.com/lsegal/aviary/releases/download/v$version/aviary_v$version_windows_amd64.tar.gz",
				},
			},
		},
	};
	await writeFile(scoopManifestPath, `${JSON.stringify(manifest, null, "\t")}\n`);
}

if (
	releaseHashes.darwin_amd64 &&
	releaseHashes.darwin_arm64 &&
	releaseHashes.linux_amd64 &&
	releaseHashes.linux_arm64
) {
	await mkdir(path.dirname(homebrewFormulaPath), { recursive: true });
	await writeFile(
		homebrewFormulaPath,
		renderHomebrewFormula(version, releaseHashes),
	);
}
