// Runs `aviary watch` from source behind the Vite dev server (pnpm dev:watch),
// so the watch session's minimal web UI gets live-reloading assets instead of
// the embedded web build. Arguments are passed through to `aviary watch`:
//
//	pnpm dev:watch -- lsegal/aviary --no-tui
//
// The backend runs under scripts/dev-go.mjs (forgo hot reload) and writes the
// port it chose to AVIARY_WATCH_PORT_FILE; Vite then starts on a free port
// from 5173 up with its proxy pointed at that port, and is restarted if a
// restarted backend picks another one. While the watch TUI owns the terminal,
// Vite's output goes to a log file instead. Ctrl+C or SIGTERM stops both; the
// backend removes its temporary session directory as it exits.
import { spawn } from "node:child_process";
import { closeSync, existsSync, openSync, readFileSync, rmSync } from "node:fs";
import net from "node:net";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { devTLSOptedOut } from "./dev-cert.mjs";

const rootDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const viteBin = path.join(rootDir, "node_modules", "vite", "bin", "vite.js");

const args = process.argv.slice(2);
if (args[0] === "--") args.shift();

// listenable reports whether port can be listened on at host.
function listenable(port, host) {
	return new Promise((resolve) => {
		const srv = net.createServer();
		srv.once("error", () => resolve(false));
		srv.listen({ port, host, ipv6Only: host === "::" }, () =>
			srv.close(() => resolve(true)),
		);
	});
}

// portFree reports whether nothing listens on port on any IPv4 or IPv6
// interface, where Vite listens (server.host). A listener on one family
// alone, such as the docs dev server's 0.0.0.0:5174, still takes the port.
async function portFree(port) {
	return (await listenable(port, "0.0.0.0")) && (await listenable(port, "::"));
}

async function freePort(start) {
	for (let port = start; port < start + 100; port++) {
		if (await portFree(port)) return port;
	}
	throw new Error(`no free port from ${start} to ${start + 99}`);
}

function readPort(file) {
	try {
		const port = Number.parseInt(readFileSync(file, "utf8").trim(), 10);
		return Number.isInteger(port) && port > 0 ? port : null;
	} catch {
		return null;
	}
}

const vitePort = await freePort(5173);
const webURL = `${devTLSOptedOut() ? "http" : "https"}://localhost:${vitePort}`;
const portFile = path.join(tmpdir(), `aviary-watch-port-${process.pid}`);
rmSync(portFile, { force: true });

// The watch TUI draws over the whole terminal, so Vite must not print there.
const tui =
	process.stdout.isTTY &&
	!args.includes("--no-tui") &&
	!args.includes("--no-tui=true");
const viteLog = path.join(tmpdir(), `aviary-dev-watch-vite-${process.pid}.log`);
if (tui) {
	console.log(`Vite dev server: ${webURL} (log: ${viteLog})`);
}

let stopping = false;
let vite = null;
let vitePortTarget = null;

const backend = spawn(
	process.execPath,
	[path.join(rootDir, "scripts", "dev-go.mjs"), "watch", ...args],
	{
		cwd: rootDir,
		env: {
			...process.env,
			AVIARY_WATCH_PORT_FILE: portFile,
			AVIARY_WATCH_WEB_URL: webURL,
		},
		stdio: "inherit",
	},
);

function startVite(backendPort) {
	vitePortTarget = backendPort;
	let logFd = null;
	let stdio = "inherit";
	if (tui) {
		logFd = openSync(viteLog, "a");
		stdio = ["ignore", logFd, logFd];
	}
	const child = spawn(
		process.execPath,
		[viteBin, "--port", String(vitePort), "--strictPort"],
		{
			cwd: rootDir,
			env: {
				...process.env,
				AVIARY_DEV_BACKEND: `http://127.0.0.1:${backendPort}`,
			},
			stdio,
		},
	);
	if (logFd !== null) closeSync(logFd);
	child.on("exit", (code, signal) => {
		if (child !== vite || stopping) return;
		// Vite died on its own: end the session rather than leave a web UI
		// that no longer loads.
		console.error(
			`error: the Vite dev server exited (${signal ?? `code ${code}`})` +
				(tui ? `; see ${viteLog}` : ""),
		);
		stop("SIGTERM");
	});
	vite = child;
}

function stopVite(signal) {
	const child = vite;
	vite = null;
	if (!child || child.exitCode !== null || child.signalCode !== null) {
		return Promise.resolve();
	}
	return new Promise((resolve) => {
		child.once("exit", resolve);
		child.kill(signal);
	});
}

// Start Vite once the backend has chosen its port, and restart it on a new
// port should a restarted backend choose another.
const poll = setInterval(async () => {
	if (stopping) return;
	const port = readPort(portFile);
	if (port === null || port === vitePortTarget) return;
	if (vite) {
		console.log(`Backend moved to port ${port}; restarting Vite.`);
		await stopVite("SIGTERM");
	}
	startVite(port);
}, 250);

function stop(signal) {
	if (stopping) return;
	stopping = true;
	clearInterval(poll);
	void stopVite(signal);
	if (backend.exitCode === null && backend.signalCode === null) {
		// SIGINT whatever the signal: forgo passes SIGINT on to the watch
		// session but exits on SIGTERM, leaving the session running with
		// its temporary directory. Both end a watch session the same way.
		backend.kill("SIGINT");
	}
}

for (const signal of ["SIGINT", "SIGTERM"]) {
	process.on(signal, () => stop(signal));
}

backend.on("exit", async (code, signal) => {
	stopping = true;
	clearInterval(poll);
	await stopVite("SIGTERM");
	rmSync(portFile, { force: true });
	if (tui && existsSync(viteLog)) rmSync(viteLog, { force: true });
	if (signal) {
		process.kill(process.pid, signal);
		return;
	}
	process.exit(code ?? 0);
});
