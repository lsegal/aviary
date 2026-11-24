import { cp, mkdir, rm, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const rootDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const webDistDir = path.join(rootDir, "web", "dist");
const embeddedDir = path.join(rootDir, "internal", "server", "webdist");
const gitkeepPath = path.join(embeddedDir, ".gitkeep");
// A backend run from source compares this with the web sources' mtimes to
// report a stale build (internal/server/webassets.go).
const stampPath = path.join(embeddedDir, ".build-stamp");

await rm(embeddedDir, { recursive: true, force: true });
await mkdir(embeddedDir, { recursive: true });
await cp(webDistDir, embeddedDir, { recursive: true });
await writeFile(gitkeepPath, "");
await writeFile(stampPath, `${new Date().toISOString()}\n`);
