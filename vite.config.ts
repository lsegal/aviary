import tailwindcss from "@tailwindcss/vite";
import vue from "@vitejs/plugin-vue";
import { defineConfig } from "vite";
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
		https: command === "serve" ? devHttps() : undefined,
		// Target the backend with the scheme and port it serves, so the proxy
		// never sends plain HTTP to its TLS listener.
		proxy: devProxy(command === "serve" ? devServerURL() : undefined),
	},
}));
