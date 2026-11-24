// Provides the TLS cert the Vite dev server serves with. It is the Aviary
// server's own cert (`aviary cert`): the configured server.tls cert/key, or
// the self-signed cert covering localhost, the hostname, and the LAN IPv4
// addresses, generated or regenerated as needed. Sharing it means a device
// trusts one cert for both the dev server and the backend.
//
// Run directly to regenerate the self-signed cert: `pnpm dev:cert`.
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { findForgo } from "./go-runner.mjs";

const rootDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

let cachedCert;

function aviaryCert(extraArgs) {
	if (extraArgs.length === 0 && cachedCert) return cachedCert;
	const bin = findForgo() ?? "go";
	const out = execFileSync(
		bin,
		["run", "./cmd/aviary", "cert", "--json", ...extraArgs],
		{ cwd: rootDir, encoding: "utf8", stdio: ["ignore", "pipe", "inherit"] },
	);
	const result = JSON.parse(out);
	if (extraArgs.length === 0) cachedCert = result;
	return result;
}

// devTLSOptedOut reports whether AVIARY_DEV_NO_TLS asks the dev server to
// serve plain HTTP.
export function devTLSOptedOut() {
	const optOut = process.env.AVIARY_DEV_NO_TLS?.trim().toLowerCase();
	return Boolean(optOut) && !["0", "false", "no"].includes(optOut);
}

// devHttps returns Vite's server.https options, or undefined to serve plain
// HTTP when AVIARY_DEV_NO_TLS is set or the cert cannot be loaded.
export function devHttps() {
	if (devTLSOptedOut()) return undefined;
	try {
		const { cert, key } = aviaryCert([]);
		return { cert: readFileSync(cert), key: readFileSync(key) };
	} catch (err) {
		console.warn(
			`warning: could not load the dev TLS cert, serving plain HTTP: ${err.message}`,
		);
		return undefined;
	}
}

// devServerURL returns the Aviary backend's URL from its config, with the
// scheme it serves, or undefined when it cannot be resolved.
export function devServerURL() {
	try {
		return aviaryCert([]).server_url;
	} catch (err) {
		console.warn(`warning: could not resolve the Aviary server URL: ${err.message}`);
		return undefined;
	}
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
	const { cert, key } = aviaryCert(["--regenerate"]);
	console.log(`cert: ${cert}\nkey:  ${key}`);
	console.log("Restart `pnpm dev` to serve the new cert.");
}
