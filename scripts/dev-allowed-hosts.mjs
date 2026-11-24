// Reads the extra dev-server hostnames from
// __VITE_ADDITIONAL_SERVER_ALLOWED_HOSTS so the web app, docs, and preview
// servers accept the same hosts (#1102). Vite 8 reads the variable itself, but
// VitePress bundles an older Vite that ignores it, so both configs set
// allowedHosts explicitly from this helper.
export const ALLOWED_HOSTS_ENV = "__VITE_ADDITIONAL_SERVER_ALLOWED_HOSTS";

// devAllowedHosts returns the comma-separated hosts in env, trimmed, with
// empty entries dropped.
export function devAllowedHosts(env = process.env) {
	return (env[ALLOWED_HOSTS_ENV] ?? "")
		.split(",")
		.map((host) => host.trim())
		.filter(Boolean);
}
