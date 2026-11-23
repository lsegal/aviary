// Locates the forgo toolchain `pnpm dev` runs the Go backend with, so other
// dev scripts run Go with the same toolchain.
import { accessSync, constants } from "node:fs";
import { execFileSync } from "node:child_process";
import { homedir } from "node:os";
import path from "node:path";

export const isWindows = process.platform === "win32";
export const exeExt = isWindows ? ".exe" : "";
export const installDir =
	process.env.FORGO_INSTALL_DIR ?? path.join(homedir(), ".forgo");

function isExecutable(file) {
	try {
		accessSync(file, constants.X_OK);
		return true;
	} catch {
		return false;
	}
}

function findOnPath() {
	try {
		const finder = isWindows ? "where" : "which";
		const found = execFileSync(finder, ["forgo"], {
			encoding: "utf8",
			stdio: ["ignore", "pipe", "ignore"],
		})
			.split(/\r?\n/)[0]
			.trim();
		return found || null;
	} catch {
		return null;
	}
}

export function findForgo() {
	if (process.env.FORGO_BIN) return process.env.FORGO_BIN;
	const installed = path.join(installDir, "bin", `forgo${exeExt}`);
	if (isExecutable(installed)) return installed;
	return findOnPath();
}
