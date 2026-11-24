import tailwindcss from "@tailwindcss/vite";
import vue from "@vitejs/plugin-vue";
import { defineConfig } from "vite";
import { devAllowedHosts } from "./scripts/dev-allowed-hosts.mjs";
import { devHttps, devServerURL } from "./scripts/dev-cert.mjs";
import { devProxy } from "./scripts/dev-proxy.mjs";

export default defineConfig(({ command }) => ({
	root: "web",
	plugins: [vue(), tailwindcss()],
	build: {
		outDir: "dist",
		emptyOutDir: true,
	},
	server: {
		// Expose the dev server on the LAN over HTTPS: browsers only allow the
		// composer mic (getUserMedia) on secure origins or localhost.
		host: true,
		// Accept the same LAN host names as the docs dev server (#1105).
		allowedHosts: devAllowedHosts(),
		https: command === "serve" ? devHttps() : undefined,
		// Target the backend with the scheme and port it serves, so the proxy
		// never sends plain HTTP to its TLS listener. scripts/dev-watch.mjs
		// points AVIARY_DEV_BACKEND at the port an `aviary watch` session chose.
		proxy: devProxy(
			process.env.AVIARY_DEV_BACKEND ||
				(command === "serve" ? devServerURL() : undefined),
		),
	},
	preview: {
		allowedHosts: devAllowedHosts(),
	},
}));
