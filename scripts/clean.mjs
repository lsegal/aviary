import { rm } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const rootDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

for (const relPath of [
	"aviary",
	"aviary.exe",
	path.join("web", "dist"),
	path.join("internal", "server", "webdist"),
	path.join("dist", "release"),
]) {
	await rm(path.join(rootDir, relPath), { recursive: true, force: true });
}
