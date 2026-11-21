// Runs the Aviary Go backend for development under forgo's hot reload
// (`forgo run --watch`), installing the forgo toolchain first if it is missing.
// A dev update only pulls the checkout and lets forgo hot-reload the backend in
// place. A backend that asks to restart (a safe restart, or a pull forgo cannot
// hot-reload, such as a go.mod change) is run again, which rebuilds it from the
// current sources. A managed forgo older than MIN_FORGO_VERSION is updated
// first, because earlier releases crash the backend after some hot reloads.
import { execFileSync, spawn } from "node:child_process";
import {
	accessSync,
	constants,
	existsSync,
	readFileSync,
	realpathSync,
	rmSync,
} from "node:fs";
import { homedir, tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const rootDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const isWindows = process.platform === "win32";
const exeExt = isWindows ? ".exe" : "";
const installDir =
	process.env.FORGO_INSTALL_DIR ?? path.join(homedir(), ".forgo");

// forgo only publishes release binaries for these platforms.
const SUPPORTED = new Set(["darwin-arm64", "linux-x64", "win32-x64"]);

// The oldest forgo whose hot reload Aviary's dev updates rely on: 0.7.4 fixed
// a crash after reloading code that uses a type (lsegal/forgo#50).
const MIN_FORGO_VERSION = "0.7.4";

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

// forgoVersion returns the version in the FORGO_VERSION file at the root of
// the toolchain bin belongs to (bin/forgo), or null. `forgo version` is no
// help: it reads that file from $GOROOT, which need not be bin's toolchain.
function forgoVersion(bin) {
	try {
		const root = path.dirname(path.dirname(realpathSync(bin)));
		return readFileSync(path.join(root, "FORGO_VERSION"), "utf8").trim();
	} catch {
		return null;
	}
}

function versionAtLeast(version, min) {
	const a = version.replace(/^v/, "").split(".").map(Number);
	const b = min.split(".").map(Number);
	for (let i = 0; i < Math.max(a.length, b.length); i++) {
		const d = (a[i] ?? 0) - (b[i] ?? 0);
		if (d !== 0) return d > 0;
	}
	return true;
}

function installForgo() {
	const url = isWindows
		? "https://github.com/lsegal/forgo/releases/latest/download/install.ps1"
		: "https://github.com/lsegal/forgo/releases/latest/download/install.sh";
	console.log(`installing forgo from ${url}`);
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
		console.log("forgo not found");
		installForgo();
		forgo = findForgo();
	}
	const version = forgo && forgoVersion(forgo);
	if (version && !versionAtLeast(version, MIN_FORGO_VERSION)) {
		const managed = forgo === path.join(installDir, "bin", `forgo${exeExt}`);
		if (managed && !process.env.FORGO_BIN) {
			console.log(
				`forgo ${version} is older than ${MIN_FORGO_VERSION}, updating it`,
			);
			installForgo();
		} else {
			console.warn(
				`warning: ${forgo} is forgo ${version}; hot reload needs forgo ` +
					`${MIN_FORGO_VERSION} or newer and may crash the backend.`,
			);
		}
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
// The backend creates this file before exiting to be run again; see
// update.RestartFileEnv.
const restartFile = path.join(tmpdir(), `aviary-restart-${process.pid}`);
rmSync(restartFile, { force: true });
const env = { ...process.env };
// The checkout root is authoritative for git mode, so the backend need not
// run inside the checkout to find it.
env.AVIARY_CHECKOUT = rootDir;
env.AVIARY_RESTART_FILE = restartFile;

let child;
for (const signal of ["SIGINT", "SIGTERM"]) {
	process.on(signal, () => child.kill(signal));
}

function run() {
	child = spawn(bin, [...args, "./cmd/aviary", ...process.argv.slice(2)], {
		cwd: rootDir,
		env,
		stdio: "inherit",
	});
	child.on("exit", (code, signal) => {
		if (existsSync(restartFile)) {
			rmSync(restartFile, { force: true });
			console.log("Restarting Aviary from the current sources...");
			run();
			return;
		}
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
}

run();
