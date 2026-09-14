import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "node:path";

// TAD §9.1 / ADR-008: React SPA (Vite), tanpa SSR; proxy /api ke backend Go saat dev.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: { alias: { "@": path.resolve(import.meta.dirname, "src") } },
  server: {
    port: 5173,
    proxy: { "/api": { target: process.env.BV_API_URL || "http://localhost:8080", changeOrigin: true }, "/public": { target: process.env.BV_API_URL || "http://localhost:8080", changeOrigin: true } },
  },
  build: {
    sourcemap: false,
    chunkSizeWarningLimit: 600,
    rollupOptions: {
      output: {
        manualChunks(id: string) {
          if (id.includes("node_modules/recharts") || id.includes("node_modules/d3-")) return "charts";
          if (id.includes("node_modules/@tanstack/react-table") || id.includes("node_modules/@tanstack/react-virtual")) return "table";
          if (id.includes("node_modules/react") || id.includes("node_modules/react-dom") || id.includes("node_modules/react-router") || id.includes("node_modules/@tanstack/react-query")) return "vendor";
          return undefined;
        },
      },
    },
  },
  test: { environment: "jsdom", globals: true, setupFiles: ["./src/test/setup.ts"] },
});
