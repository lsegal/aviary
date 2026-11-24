import assert from "node:assert/strict";
import { test } from "node:test";
import { DEFAULT_SERVER_URL, devProxy } from "./dev-proxy.mjs";

test("proxies HTTP and websockets over TLS to a TLS backend", () => {
	const proxy = devProxy("https://localhost:16677");
	for (const route of ["/mcp", "/api"]) {
		assert.equal(proxy[route].target, "https://localhost:16677");
		assert.equal(proxy[route].headers.Origin, "https://localhost:16677");
		assert.equal(proxy[route].secure, false);
	}
	assert.equal(proxy["/api"].ws, true);
});

test("proxies plain HTTP to a no_tls backend on its configured port", () => {
	const proxy = devProxy("http://localhost:18080");
	for (const route of ["/mcp", "/api"]) {
		assert.equal(proxy[route].target, "http://localhost:18080");
		assert.equal(proxy[route].headers.Origin, "http://localhost:18080");
	}
});

test("defaults to the backend's default TLS URL", () => {
	assert.equal(devProxy()["/api"].target, DEFAULT_SERVER_URL);
});
