import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { execFileSync } from "node:child_process";
import { tmpdir } from "node:os";
import path from "node:path";

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

const args = parseArgs(process.argv.slice(2));
const root = path.resolve(args.root ?? "mock-installer-fixture");
const repo = args.repo ?? "lsegal/aviary";
const version = args.version ?? "v0.0.0-test";
const goos = args.os ?? "linux";
const goarch = args.arch ?? "amd64";
const baseUrl = args.baseUrl ?? "http://127.0.0.1:8765";

const binaryName = goos === "windows" ? "aviary.exe" : "aviary";
const assetName = `aviary_${version}_${goos}_${goarch}.tar.gz`;
const assetUrl = `${baseUrl}/assets/${assetName}`;
const release = {
	tag_name: version,
	assets: [
		{
			name: assetName,
			browser_download_url: assetUrl,
		},
	],
};

const assetsDir = path.join(root, "assets");
const releaseDir = path.join(root, "repos", ...repo.split("/"), "releases");
const stageDir = await mkdtemp(path.join(tmpdir(), "aviary-installer-fixture-"));

try {
	await mkdir(assetsDir, { recursive: true });
	await mkdir(path.join(releaseDir, "tags"), { recursive: true });

	const binaryPath = path.join(stageDir, binaryName);
	const binaryContents =
		goos === "windows"
			? Buffer.concat([Buffer.from([0x4d, 0x5a]), Buffer.from("mock aviary windows binary\n")])
			: "#!/usr/bin/env sh\necho mock aviary\n";
	await writeFile(binaryPath, binaryContents, {
		mode: goos === "windows" ? 0o644 : 0o755,
	});

	execFileSync("tar", ["-czf", path.join(assetsDir, assetName), "-C", stageDir, binaryName], {
		stdio: "inherit",
	});

	const releaseJSON = `${JSON.stringify(release, null, 2)}\n`;
	await writeFile(path.join(releaseDir, "latest"), releaseJSON);
	await writeFile(path.join(releaseDir, "tags", version), releaseJSON);
} finally {
	await rm(stageDir, { recursive: true, force: true });
}
