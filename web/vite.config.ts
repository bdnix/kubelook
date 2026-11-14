/// <reference types="vitest/config" />
import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: [
      // monaco-editor 0.57 maps its subpaths through "exports"; monaco-worker-manager (a dependency of
      // monaco-yaml) still imports the old file path. Point it at the file.
      {
        find: /^monaco-editor\/esm\/vs\/(.*)$/,
        replacement: `${fileURLToPath(new URL('./node_modules/monaco-editor/esm/vs', import.meta.url))}/$1`,
      },
    ],
  },
  build: { outDir: 'dist', sourcemap: false },
  server: { proxy: { '/api': 'http://localhost:8080', '/auth': 'http://localhost:8080' } },
  test: {
    environment: 'jsdom',
    setupFiles: ['src/test/setup.ts'],
    restoreMocks: true,
    coverage: {
      provider: 'v8',
      reporter: ['text', 'text-summary', 'json-summary', 'lcov'],
      include: ['src/**/*.{ts,tsx}'],
      exclude: [
        'src/main.tsx',
        'src/adapters/monaco/**',
        'src/adapters/xterm/**',
        'src/test/**',
        'src/**/*.test.{ts,tsx}',
        'src/**/*.d.ts',
      ],
      thresholds: { lines: 90, statements: 90, functions: 90, branches: 80 },
    },
  },
});
