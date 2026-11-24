// Builds the Vite dev server's proxy to the Aviary backend. The target is the
// backend's own URL (`aviary cert --json` reports it as server_url), so the
// proxy speaks TLS to a TLS server and plain HTTP to a no_tls one instead of
// failing every handshake.
export const DEFAULT_SERVER_URL = "https://localhost:16677";

// devProxy returns Vite's server.proxy options for the backend at serverURL.
// The backend serves a self-signed cert by default, so the proxy does not
// verify it.
export function devProxy(serverURL = DEFAULT_SERVER_URL) {
	const target = new URL(serverURL).origin;
	const common = {
		target,
		secure: false,
		changeOrigin: true,
		headers: { Origin: target },
	};
	return {
		"/mcp": { ...common },
		"/api": { ...common, ws: true },
	};
}
