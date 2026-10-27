/// <reference types="vitest/config" />
import { fileURLToPath, URL } from 'node:url'
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig, type ProxyOptions } from 'vite'

const backend = process.env.SHAREDCAL_BACKEND ?? 'http://localhost:8080'

const proxy: ProxyOptions = {
  target: backend,
  changeOrigin: false,
  // Keep the browser's Origin so the server's same-origin check sees the dev URL.
  xfwd: true,
}

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
  },
  server: {
    port: 5173,
    proxy: {
      '/api/stream': {
        ...proxy,
        // Server-Sent Events: never buffer or compress the response.
        configure: (p) => {
          p.on('proxyReq', (req) => req.setHeader('Accept-Encoding', 'identity'))
          p.on('proxyRes', (res) => {
            res.headers['cache-control'] = 'no-cache'
            res.headers['x-accel-buffering'] = 'no'
          })
        },
      },
      '/api': proxy,
      '/auth': proxy,
      '/ics': proxy,
      '/branding': proxy,
      '/healthz': proxy,
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    target: 'es2022',
    sourcemap: false,
    manifest: true,
    // The real budget (initial JS < 300 KB gzip) is enforced by `npm run check:size`.
    chunkSizeWarningLimit: 1000,
  },
  test: {
    globals: true,
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    include: ['src/**/*.test.{ts,tsx}'],
    css: false,
  },
})
