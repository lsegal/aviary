// Runs the Aviary Go backend for development under forgo's hot reload
// (`forgo run --watch`), installing the forgo toolchain first if it is missing.
import { execFileSync, spawn } from "node:child_process";
import { accessSync, constants } from "node:fs";
import { homedir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const rootDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const isWindows = process.platform === "win32";
const exeExt = isWindows ? ".exe" : "";
const installDir =
	process.env.FORGO_INSTALL_DIR ?? path.join(homedir(), ".forgo");

// forgo only publishes release binaries for these platforms.
const SUPPORTED = new Set(["darwin-arm64", "linux-x64", "win32-x64"]);

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

function findForgo() {
	if (process.env.FORGO_BIN) return process.env.FORGO_BIN;
	const installed = path.join(installDir, "bin", `forgo${exeExt}`);
	if (isExecutable(installed)) return installed;
	return findOnPath();
}

function installForgo() {
	const url = isWindows
		? "https://github.com/lsegal/forgo/releases/latest/download/install.ps1"
		: "https://github.com/lsegal/forgo/releases/latest/download/install.sh";
	console.log(`forgo not found, installing from ${url}`);
	const [cmd, args] = isWindows
		? ["powershell", ["-NoProfile", "-Command", `irm ${url} | iex`]]
		: ["sh", ["-c", `curl -fsSL ${url} | sh`]];
	execFileSync(cmd, args, { cwd: rootDir, stdio: "inherit" });
}

function resolveRunner() {
	const platformKey = `${process.platform}-${process.arch}`;
	if (!SUPPORTED.has(platformKey)) {
		console.warn(
			`warning: forgo publishes no release for ${platformKey}; ` +
				"falling back to 'go run' without hot reload.",
		);
		return { bin: "go", args: ["run"] };
	}

	let forgo = findForgo();
	if (!forgo) {
		installForgo();
		forgo = findForgo();
	}
	if (!forgo) {
		console.error(
			"error: forgo installation finished but no forgo binary was found. " +
				`Install it manually (see https://github.com/lsegal/forgo) or set FORGO_BIN.`,
		);
		process.exit(1);
	}
	return { bin: forgo, args: ["run", "--watch"] };
}

const { bin, args } = resolveRunner();
const child = spawn(bin, [...args, "./cmd/aviary", ...process.argv.slice(2)], {
	cwd: rootDir,
	stdio: "inherit",
});

for (const signal of ["SIGINT", "SIGTERM"]) {
	process.on(signal, () => child.kill(signal));
}

child.on("exit", (code, signal) => {
	if (signal) {
		process.kill(process.pid, signal);
		return;
	}
	process.exit(code ?? 0);
});
child.on("error", (err) => {
	console.error(`error: failed to start ${bin}: ${err.message}`);
	process.exit(1);
});
