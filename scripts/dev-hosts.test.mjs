import assert from "node:assert/strict";
import { test } from "node:test";
import { devHosts } from "./dev-hosts.mjs";

const interfaces = {
	lo0: [{ address: "127.0.0.1", family: "IPv4", internal: true }],
	en0: [
		{ address: "fe80::1", family: "IPv6", internal: false },
		{ address: "192.168.1.20", family: "IPv4", internal: false },
	],
	en1: [{ address: "169.254.3.4", family: "IPv4", internal: false }],
	en2: [{ address: "10.0.0.5", family: 4, internal: false }],
};

test("defaults to localhost, the hostname, its .local name, and LAN IPv4s", () => {
	assert.deepEqual(devHosts({ env: {}, hostname: "LLS", interfaces }), [
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
		devHosts({ env: {}, hostname: "box.example.com", interfaces: {} }),
		["localhost", "127.0.0.1", "::1", "box.example.com"],
	);
});

test("adds AVIARY_DEV_HOSTS and Vite's additional allowed hosts", () => {
	const hosts = devHosts({
		env: {
			AVIARY_DEV_HOSTS: " Dev.Example.com, .tailnet.ts.net ,,",
			__VITE_ADDITIONAL_SERVER_ALLOWED_HOSTS: "lls.local,other.local",
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
