import tailwindcss from "@tailwindcss/vite";
import vue from "@vitejs/plugin-vue";
import { defineConfig } from "vite";
import { devHttps } from "./scripts/dev-cert.mjs";

// The Go backend the dev server proxies to. scripts/dev-watch.mjs points it
// at the port an `aviary watch` session chose.
const backend = process.env.AVIARY_DEV_BACKEND || "https://localhost:16677";

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
		proxy: {
			"/mcp": {
				target: backend,
				secure: false,
				changeOrigin: true,
				headers: {
					Origin: backend,
				},
			},
			"/api": {
				target: backend,
				secure: false,
				changeOrigin: true,
				ws: true,
				headers: {
					Origin: backend,
				},
			},
		},
	},
}));
