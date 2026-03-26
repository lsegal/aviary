import { $ } from "execa";
import { readFile, readdir } from "node:fs/promises";

const pkgData = await readFile("package.json");
const pkg = JSON.parse(pkgData.toString());
const pkgfile = `${pkg.name}-v${pkg.version}.tgz`;
const assets = (await readdir("dist/release"))
	.filter((name) => name.endsWith(".tar.gz") || name === "checksums.txt")
	.map((name) => path.posix.join("dist/release", name));

if (assets.length === 0) {
	throw new Error("no release assets found in dist/release");
}

const opts = { shell: true, stderr: process.stderr, stdout: process.stdout };
await $(opts)`git push origin main v${pkg.version}`;
await $(opts)`gh release create --generate-notes v${pkg.version} ${assets.join(" ")}`;
