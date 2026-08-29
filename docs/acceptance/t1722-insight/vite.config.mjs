import { fileURLToPath, URL } from 'node:url';
import { createRequire } from 'node:module';

const require = createRequire(new URL('../../../web/package.json', import.meta.url));
const react = require('@vitejs/plugin-react');
const { defineConfig } = require('vite');

export default defineConfig({
  root: fileURLToPath(new URL('../../../web', import.meta.url)),
  plugins: [react.default()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('../../../web/src', import.meta.url)),
    },
  },
  server: {
    port: 5174,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:17100',
        changeOrigin: false,
      },
    },
  },
});
