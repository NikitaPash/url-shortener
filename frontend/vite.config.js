import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  base: '/app/',
  server: {
    port: 5173,
    host: true,
    // Used only when running `npm run dev` locally outside Docker.
    // In Docker mode the browser hits nginx which routes /auth and /api to go-api.
    proxy: {
      '/auth': 'http://localhost',
      '/api': 'http://localhost',
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: './src/setupTests.js',
    // Prevent OOM in CI by limiting workers.
    pool: 'forks',
  },
})
