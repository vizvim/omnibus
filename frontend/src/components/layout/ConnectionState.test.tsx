import { render, screen } from "@testing-library/react";
import { axe } from "jest-axe";
import { describe, expect, it } from "vitest";

import { ConnectionStateIndicator } from "./ConnectionState";

describe("ConnectionStateIndicator", () => {
  it("renders the live state with a mono label and an aria-live region", () => {
    const { container } = render(<ConnectionStateIndicator state="live" />);
    expect(screen.getByText("live")).toBeInTheDocument();
    // The indicator is wrapped in a polite live region so transitions are announced.
    const region = container.querySelector('[aria-live="polite"]');
    expect(region).not.toBeNull();
    // Not busy when live.
    expect(container.querySelector('[aria-busy="true"]')).toBeNull();
  });

  it("renders the reconnecting state with aria-busy and a spinner", () => {
    const { container } = render(<ConnectionStateIndicator state="reconnecting" />);
    expect(screen.getByText("reconnecting…")).toBeInTheDocument();
    expect(container.querySelector('[aria-busy="true"]')).not.toBeNull();
    // An aria-live announcement describes the degraded state for screen readers.
    expect(screen.getByText(/Reconnecting to live updates/i)).toBeInTheDocument();
  });

  it("renders the offline state with a label and an announcement", () => {
    render(<ConnectionStateIndicator state="offline" />);
    expect(screen.getByText("offline")).toBeInTheDocument();
    expect(
      screen.getByText(/Live updates disconnected\. Showing last-known status\./i),
    ).toBeInTheDocument();
  });

  it("has no axe violations across all three states", async () => {
    for (const state of ["live", "reconnecting", "offline"] as const) {
      const { container, unmount } = render(<ConnectionStateIndicator state={state} />);
      expect(await axe(container)).toHaveNoViolations();
      unmount();
    }
  });
});
