import tailwindcss from "@tailwindcss/vite";
import vue from "@vitejs/plugin-vue";
import { defineConfig } from "vite";
import { devHttps } from "./scripts/dev-cert.mjs";

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
				target: "https://localhost:16677",
				secure: false,
				changeOrigin: true,
				headers: {
					Origin: "https://localhost:16677",
				},
			},
			"/api": {
				target: "https://localhost:16677",
				secure: false,
				changeOrigin: true,
				ws: true,
				headers: {
					Origin: "https://localhost:16677",
				},
			},
		},
	},
}));
