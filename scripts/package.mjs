import { $ } from "execa";
import { readFile } from "node:fs/promises";

const opts = { shell: true, stderr: process.stderr, stdout: process.stdout };

await $(opts)`npm version ${
  process.env.VERSION || "minor"
} --no-commit-hooks --no-git-tag-version`;

const pkgData = await readFile("package.json");
const pkg = JSON.parse(pkgData.toString());
const ver = pkg.version;
await $(opts)`pnpm build:release -- --version v${ver}`;
await $(opts)`git add . && git status`;
await $(opts)`pnpm exec vpr release-commit ${ver}`;
await $(opts)`git --no-pager show`;
