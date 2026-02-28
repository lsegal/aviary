import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/mcp': {
        target: 'https://localhost:16677',
        secure: false,
        changeOrigin: true,
      },
      '/api': {
        target: 'https://localhost:16677',
        secure: false,
        changeOrigin: true,
      },
    },
  },
})
