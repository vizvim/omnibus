import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

// Unmount React trees between tests so the DOM does not leak across cases.
afterEach(() => {
  cleanup();
});
