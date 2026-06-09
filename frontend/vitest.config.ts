import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

// Vitest config for component tests: jsdom environment + Testing Library setup, with
// the React plugin so JSX/TSX compiles the same way the app build does.
export default defineConfig({
  plugins: [react()],
  test: {
    globals: true,
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    css: true,
  },
});
