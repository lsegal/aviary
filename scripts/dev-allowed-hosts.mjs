// Provides the host names the Vite servers (the web UI dev and preview servers
// and the VitePress docs dev server) accept requests for, so they are
// reachable from other devices on the LAN by the same names (#1102, #1105).
// The defaults match the self-signed dev cert's SANs (SelfSignedSANs in
// internal/server/tls.go): localhost, the loopback addresses, the hostname,
// its .local mDNS name, and the LAN IPv4 addresses.
//
// AVIARY_DEV_HOSTS adds comma-separated names, such as a DNS name pointing at
// the machine; a leading dot allows every subdomain, as in Vite's
// server.allowedHosts. Vite's own __VITE_ADDITIONAL_SERVER_ALLOWED_HOSTS is
// honored too: Vite 8 reads it itself, but VitePress bundles an older Vite
// that ignores it, so both configs set allowedHosts explicitly from here.
import os from "node:os";

export const DEV_HOSTS_ENV = "AVIARY_DEV_HOSTS";
export const ALLOWED_HOSTS_ENV = "__VITE_ADDITIONAL_SERVER_ALLOWED_HOSTS";

// splitHosts returns the comma-separated names in value, trimmed and
// lowercased, with empty entries dropped.
export function splitHosts(value) {
	return (value ?? "")
		.split(",")
		.map((name) => name.trim().toLowerCase())
		.filter(Boolean);
}

// lanIPv4s returns the IPv4 addresses of the host's non-loopback,
// non-link-local interfaces.
function lanIPv4s(interfaces) {
	const ips = [];
	for (const addrs of Object.values(interfaces)) {
		for (const addr of addrs ?? []) {
			const family = addr.family === 4 ? "IPv4" : addr.family;
			if (family !== "IPv4" || addr.internal) continue;
			if (addr.address.startsWith("169.254.")) continue;
			ips.push(addr.address);
		}
	}
	return ips;
}

// devAllowedHosts returns the servers' allowedHosts list.
export function devAllowedHosts({
	env = process.env,
	hostname = os.hostname(),
	interfaces = os.networkInterfaces(),
} = {}) {
	const hosts = ["localhost", "127.0.0.1", "::1"];
	const host = hostname?.trim().toLowerCase();
	if (host) {
		hosts.push(host);
		// Bare hostnames are usually reachable over mDNS as host.local.
		if (!host.includes(".")) hosts.push(`${host}.local`);
	}
	hosts.push(...lanIPv4s(interfaces));
	hosts.push(...splitHosts(env[DEV_HOSTS_ENV]));
	hosts.push(...splitHosts(env[ALLOWED_HOSTS_ENV]));
	return [...new Set(hosts)];
}
