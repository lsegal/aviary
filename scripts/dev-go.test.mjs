import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import {
	chmodSync,
	existsSync,
	mkdtempSync,
	readFileSync,
	rmSync,
	writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

const devGo = path.join(path.dirname(fileURLToPath(import.meta.url)), "dev-go.mjs");

// A stand-in for forgo during its startup handshake: it starts the backend,
// then exits on SIGINT without passing it on.
const fakeForgo = `#!/usr/bin/env node
const { spawn } = require("node:child_process");
spawn(process.execPath, ["-e", ${JSON.stringify(
	"require('node:fs').writeFileSync(process.env.BACKEND_PID_FILE, String(process.pid)); setInterval(() => {}, 1000);",
)}], { stdio: "ignore" });
setInterval(() => {}, 1000);
`;

function alive(pid) {
	try {
		process.kill(pid, 0);
		return true;
	} catch {
		return false;
	}
}

async function waitFor(check, timeoutMs = 10_000) {
	const deadline = Date.now() + timeoutMs;
	while (!check()) {
		if (Date.now() > deadline) throw new Error("timed out");
		await new Promise((resolve) => setTimeout(resolve, 50));
	}
}

const supported = ["darwin-arm64", "linux-x64"].includes(
	`${process.platform}-${process.arch}`,
);

for (const signal of ["SIGINT", "SIGTERM"]) {
	test(
		`${signal} stops a backend forgo leaves behind`,
		{ skip: !supported && "forgo does not run here" },
		async (t) => {
			const dir = mkdtempSync(path.join(tmpdir(), "dev-go-test-"));
			t.after(() => rmSync(dir, { recursive: true, force: true }));
			const forgo = path.join(dir, "forgo");
			writeFileSync(forgo, fakeForgo);
			chmodSync(forgo, 0o755);
			const pidFile = path.join(dir, "backend.pid");

			const node = spawn(process.execPath, [devGo], {
				env: { ...process.env, FORGO_BIN: forgo, BACKEND_PID_FILE: pidFile },
				stdio: "ignore",
			});
			const exited = new Promise((resolve) => node.on("exit", resolve));
			await waitFor(() => existsSync(pidFile) && readFileSync(pidFile, "utf8"));
			const backend = Number(readFileSync(pidFile, "utf8"));
			t.after(() => alive(backend) && process.kill(backend, "SIGKILL"));

			node.kill(signal);
			await exited;
			assert.equal(alive(backend), false, "backend is still running");
		},
	);
}
