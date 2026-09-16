import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "node:path";

// BuildingVision public website (Website PRD v1.1). Static site: `vite build` (client) + `vite build --ssr` + scripts/prerender.mjs
// menghasilkan HTML per route (SEO §39) yang kemudian di-hydrate. Data dinamis (App Downloads, Plans) diambil dari public API.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(import.meta.dirname, "src"),
      // hanya logo dari lapisan bv (tanpa menarik komponen dashboard/motion ke bundle website)
      "@bv/logo": path.resolve(import.meta.dirname, "../packages/ui/src/bv/BuildingVisionLogo.tsx"),
    },
    dedupe: ["react", "react-dom"],
  },
  server: {
    port: 5175,
    fs: { allow: [path.resolve(import.meta.dirname, ".."), path.resolve(import.meta.dirname)] },
    proxy: { "/api": { target: process.env.BV_API_URL || "http://localhost:8080", changeOrigin: true } },
  },
  build: { sourcemap: false, chunkSizeWarningLimit: 600 },
});
