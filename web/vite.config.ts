import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import { fileURLToPath, URL } from 'node:url';

// Vite config. Dev server proxies `/api` to the agent-center daemon
// (loopback bind, default 127.0.0.1:7100) so SSE + REST work without
// CORS gymnastics during local development. Production build is served
// by the Go binary itself via go:embed (F15).
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:7100',
        changeOrigin: false,
      },
    },
  },
  build: {
    // Output directly into the Go embed package so `make build-backend`
    // can bake the SPA into the binary without an intermediate copy.
    // The `.gitkeep` placeholder there keeps the directory in git so
    // `go:embed` succeeds even on a fresh clone before `pnpm run build`
    // has populated it.
    outDir: '../internal/webconsole/spa/dist',
    emptyOutDir: true,
    target: 'es2022',
  },
});
