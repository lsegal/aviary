import { $ } from "execa";
import { readFile, readdir } from "node:fs/promises";
import { join } from "node:path";
import { publishSignedRelease, releaseToken } from "./signed-release-commit.mjs";

const pkgData = await readFile("package.json");
const pkg = JSON.parse(pkgData.toString());
const assets = (await readdir("dist/release"))
	.filter((name) => name.endsWith(".tar.gz") || ["checksums.txt", "install.sh", "install.ps1"].includes(name))
	.map((name) => join("dist/release", name));

if (assets.length === 0) {
	throw new Error("no release assets found in dist/release");
}

const opts = { shell: true, stderr: process.stderr, stdout: process.stdout };
const tag = `v${pkg.version}`;

const auth = releaseToken();
if (auth && process.env.GITHUB_REPOSITORY) {
	// A plain git push from CI would publish an unsigned commit (#858).
	await publishSignedRelease({
		repo: process.env.GITHUB_REPOSITORY,
		token: auth.token,
		tokenSource: auth.source,
		branch: "main",
		tag,
		apiUrl: process.env.GITHUB_API_URL,
	});
} else {
	await $(opts)`git push origin main ${tag}`;
}
await $(opts)`gh release create --generate-notes ${tag} ${assets.join(" ")}`;
