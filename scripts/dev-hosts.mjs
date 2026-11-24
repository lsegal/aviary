// Provides the host names the Vite dev servers (the web UI and the VitePress
// docs) accept requests for, so both are reachable from other devices on the
// LAN by the same names. The defaults match the self-signed dev cert's SANs
// (SelfSignedSANs in internal/server/tls.go): localhost, the loopback
// addresses, the hostname, its .local mDNS name, and the LAN IPv4 addresses.
//
// AVIARY_DEV_HOSTS adds comma-separated names, such as a DNS name pointing at
// the machine; a leading dot allows every subdomain, as in Vite's
// server.allowedHosts. Vite's own __VITE_ADDITIONAL_SERVER_ALLOWED_HOSTS is
// honored too, since the docs server's older bundled Vite ignores it.
import os from "node:os";

function splitHosts(value) {
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

// devHosts returns the dev servers' server.allowedHosts list.
export function devHosts({
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
	hosts.push(...splitHosts(env.AVIARY_DEV_HOSTS));
	hosts.push(...splitHosts(env.__VITE_ADDITIONAL_SERVER_ALLOWED_HOSTS));
	return [...new Set(hosts)];
}
