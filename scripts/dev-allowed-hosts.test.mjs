import assert from "node:assert/strict";
import { existsSync } from "node:fs";
import { test } from "node:test";
import {
	ALLOWED_HOSTS_ENV,
	DEV_HOSTS_ENV,
	devAllowedHosts,
	splitHosts,
} from "./dev-allowed-hosts.mjs";

const root = new URL("../", import.meta.url);

// The configs read the variables when they load, so set them before importing.
process.env[DEV_HOSTS_ENV] = "dev.example.com";
process.env[ALLOWED_HOSTS_ENV] = " myhost.local, ,other.lan ,";

const interfaces = {
	lo0: [{ address: "127.0.0.1", family: "IPv4", internal: true }],
	en0: [
		{ address: "fe80::1", family: "IPv6", internal: false },
		{ address: "192.168.1.20", family: "IPv4", internal: false },
	],
	en1: [{ address: "169.254.3.4", family: "IPv4", internal: false }],
	en2: [{ address: "10.0.0.5", family: 4, internal: false }],
};

test("parses comma-separated hosts, ignoring whitespace and empty entries", () => {
	assert.deepEqual(splitHosts(" MyHost.local, ,other.lan ,"), [
		"myhost.local",
		"other.lan",
	]);
	assert.deepEqual(splitHosts(undefined), []);
	assert.deepEqual(splitHosts(" , "), []);
});

test("defaults to localhost, the hostname, its .local name, and LAN IPv4s", () => {
	assert.deepEqual(devAllowedHosts({ env: {}, hostname: "LLS", interfaces }), [
		"localhost",
		"127.0.0.1",
		"::1",
		"lls",
		"lls.local",
		"192.168.1.20",
		"10.0.0.5",
	]);
});

test("does not add .local to a dotted hostname", () => {
	assert.deepEqual(
		devAllowedHosts({ env: {}, hostname: "box.example.com", interfaces: {} }),
		["localhost", "127.0.0.1", "::1", "box.example.com"],
	);
});

test("adds AVIARY_DEV_HOSTS and Vite's additional allowed hosts", () => {
	const hosts = devAllowedHosts({
		env: {
			[DEV_HOSTS_ENV]: " Dev.Example.com, .tailnet.ts.net ,,",
			[ALLOWED_HOSTS_ENV]: "lls.local,other.local",
		},
		hostname: "lls",
		interfaces: {},
	});
	assert.deepEqual(hosts.slice(5), [
		"dev.example.com",
		".tailnet.ts.net",
		"other.local",
	]);
	assert.equal(hosts.filter((h) => h === "lls.local").length, 1);
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
		const hosts = devAllowedHosts();
		assert.ok(hosts.includes("myhost.local"));
		assert.ok(hosts.includes("dev.example.com"));
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
		const hosts = devAllowedHosts();
		assert.deepEqual(config.vite.server.allowedHosts, hosts);
		assert.deepEqual(config.vite.preview.allowedHosts, hosts);
	},
);
