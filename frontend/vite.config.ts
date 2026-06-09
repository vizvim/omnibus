import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "vite";

// Vite dev server proxies API + cover requests to the Go backend (h2c on :8080),
// so the browser talks same-origin in dev and the Connect transport uses /api.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    proxy: {
      "/api": "http://localhost:8080",
      "/covers": "http://localhost:8080",
    },
  },
});
