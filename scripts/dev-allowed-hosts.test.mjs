import assert from "node:assert/strict";
import { existsSync } from "node:fs";
import { test } from "node:test";
import { ALLOWED_HOSTS_ENV, devAllowedHosts } from "./dev-allowed-hosts.mjs";

const root = new URL("../", import.meta.url);
const hosts = ["myhost.local", "other.lan"];

// The configs read the variable when they load, so set it before importing.
process.env[ALLOWED_HOSTS_ENV] = " myhost.local, ,other.lan ,";

test("parses comma-separated hosts, ignoring whitespace and empty entries", () => {
	assert.deepEqual(devAllowedHosts(), hosts);
	assert.deepEqual(devAllowedHosts({}), []);
	assert.deepEqual(devAllowedHosts({ [ALLOWED_HOSTS_ENV]: " , " }), []);
});

// Each config test needs its package's dependencies: CI installs the root
// ones for test:scripts and the docs ones for the docs build.
test(
	"web app dev and preview servers allow the hosts",
	{ skip: !existsSync(new URL("node_modules/vite", root)) },
	async () => {
		const { default: config } = await import(
			new URL("vite.config.ts", root).href
		);
		const resolved = config({ command: "build", mode: "production" });
		assert.deepEqual(resolved.server.allowedHosts, hosts);
		assert.deepEqual(resolved.preview.allowedHosts, hosts);
	},
);

test(
	"docs dev and preview servers allow the hosts",
	{ skip: !existsSync(new URL("docs/node_modules/vitepress", root)) },
	async () => {
		const { default: config } = await import(
			new URL("docs/site/.vitepress/config.mts", root).href
		);
		assert.deepEqual(config.vite.server.allowedHosts, hosts);
		assert.deepEqual(config.vite.preview.allowedHosts, hosts);
	},
);
