import tailwindcss from "@tailwindcss/vite";
import vue from "@vitejs/plugin-vue";
import { defineConfig } from "vite";

export default defineConfig({
	root: "web",
	plugins: [vue(), tailwindcss()],
	build: {
		outDir: "dist",
		emptyOutDir: true,
	},
	server: {
		proxy: {
			"/mcp": {
				target: "https://localhost:16677",
				secure: false,
				changeOrigin: true,
			},
			"/api": {
				target: "https://localhost:16677",
				secure: false,
				changeOrigin: true,
				ws: true,
			},
		},
	},
});
