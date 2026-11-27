import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import { tmpdir } from "node:os";
import path from "node:path";
import { parseArgs } from "./lib/args.mjs";

const args = parseArgs(process.argv.slice(2));
const root = path.resolve(args.root ?? "mock-installer-fixture");
const repo = args.repo ?? "lsegal/aviary";
const version = args.version ?? "v0.0.0-test";
const goos = args.os ?? "linux";
const goarch = args.arch ?? "amd64";
const baseUrl = args.baseUrl ?? "http://127.0.0.1:8765";

const binaryName = goos === "windows" ? "aviary.exe" : "aviary";
const assetName = `aviary_${version}_${goos}_${goarch}.tar.gz`;

// Mirrors github.com: <repo>/releases/latest redirects to the tag page, and
// assets download from <repo>/releases/download/<tag>/<asset>.
const releasesDir = path.join(root, ...repo.split("/"), "releases");
const downloadDir = path.join(releasesDir, "download", version);
const stageDir = await mkdtemp(path.join(tmpdir(), "aviary-installer-fixture-"));

try {
	await mkdir(downloadDir, { recursive: true });

	const binaryPath = path.join(stageDir, binaryName);
	const binaryContents =
		goos === "windows"
			? Buffer.concat([Buffer.from([0x4d, 0x5a]), Buffer.from("mock aviary windows binary\n")])
			: '#!/usr/bin/env sh\nif [ "$1" = install-skills ]; then mkdir -p "$HOME/.agents/skills/gh-fix" && echo "installed gh-fix" >>"$HOME/.agents/skills/gh-fix/install.log"; fi\necho mock aviary\n';
	await writeFile(binaryPath, binaryContents, {
		mode: goos === "windows" ? 0o644 : 0o755,
	});

	const archivePath = path.join(downloadDir, assetName);
	execFileSync("tar", ["-czf", archivePath, "-C", stageDir, binaryName], {
		stdio: "inherit",
	});

	const sha256 = createHash("sha256").update(await readFile(archivePath)).digest("hex");
	await writeFile(path.join(downloadDir, "checksums.txt"), `${sha256}  ${assetName}\n`);
	await writeFile(path.join(releasesDir, "latest.redirect"), `${baseUrl}/${repo}/releases/tag/${version}\n`);
} finally {
	await rm(stageDir, { recursive: true, force: true });
}
