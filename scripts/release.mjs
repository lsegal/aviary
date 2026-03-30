import { $ } from "execa";
import { readFile, readdir } from "node:fs/promises";
import { join } from "node:path";

const pkgData = await readFile("package.json");
const pkg = JSON.parse(pkgData.toString());
const assets = (await readdir("dist/release"))
	.filter((name) => name.endsWith(".tar.gz") || name === "checksums.txt")
	.map((name) => join("dist/release", name));

if (assets.length === 0) {
	throw new Error("no release assets found in dist/release");
}

const opts = { shell: true, stderr: process.stderr, stdout: process.stdout };
const pushTarget =
	process.env.GH_TOKEN && process.env.GITHUB_REPOSITORY
		? `https://x-access-token:${process.env.GH_TOKEN}@github.com/${process.env.GITHUB_REPOSITORY}.git`
		: "origin";

await $(opts)`git push ${pushTarget} main v${pkg.version}`;
await $(opts)`gh release create --generate-notes v${pkg.version} ${assets.join(" ")}`;
