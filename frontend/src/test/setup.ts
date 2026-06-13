import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { toHaveNoViolations } from "jest-axe";
import { afterEach, expect } from "vitest";

// Register the jest-axe matcher globally so every component test can assert
// `expect(container).toHaveNoViolations()` (D-12 accessibility gate). The axe
// assertions themselves are written in Plan 05; this only installs the matcher.
expect.extend(toHaveNoViolations);

// Unmount React trees between tests so the DOM does not leak across cases.
afterEach(() => {
  cleanup();
});
