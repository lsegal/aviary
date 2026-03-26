import { execSync } from "node:child_process";
import { readdir, readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const rootDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

function run(command) {
	execSync(command, {
		cwd: rootDir,
		stdio: "inherit",
		shell: true,
	});
}

const pkg = JSON.parse(
	await readFile(path.join(rootDir, "package.json"), "utf8"),
);
const version = pkg.version;
const tag = `v${version}`;
const releaseDir = path.join(rootDir, "dist", "release");
const assets = (await readdir(releaseDir))
	.filter((name) => name.endsWith(".tar.gz") || name === "checksums.txt")
	.map((name) => path.posix.join("dist/release", name));

if (assets.length === 0) {
	throw new Error("no release assets found in dist/release");
}

run(`git push origin main ${tag}`);
run(`gh release create --generate-notes ${tag} ${assets.join(" ")}`);
