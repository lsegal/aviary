import { execSync } from "node:child_process";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const rootDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const versionSpec = process.env.VERSION || "patch";

function run(command) {
	execSync(command, {
		cwd: rootDir,
		stdio: "inherit",
		shell: true,
	});
}

run(`npm version ${versionSpec} --no-commit-hooks --no-git-tag-version`);

const pkg = JSON.parse(
	await readFile(path.join(rootDir, "package.json"), "utf8"),
);
const version = pkg.version;

run("git add .");
run("git status");
run("pnpm install --frozen-lockfile --force");
run(`pnpm build:release -- --version v${version}`);
run(`pnpm exec vpr release-commit ${version}`);
run("git --no-pager show");
