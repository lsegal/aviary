import { cp, mkdir, rm } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const rootDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const webDistDir = path.join(rootDir, "web", "dist");
const embeddedDir = path.join(rootDir, "internal", "server", "webdist");

await rm(embeddedDir, { recursive: true, force: true });
await mkdir(embeddedDir, { recursive: true });
await cp(webDistDir, embeddedDir, { recursive: true });
