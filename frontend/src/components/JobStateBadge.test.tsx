import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { JobRunState } from "../gen/omnibus/v1/jobs_pb";
import { JobStateBadge } from "./JobStateBadge";

describe("JobStateBadge", () => {
  const cases: [JobRunState, string][] = [
    [JobRunState.QUEUED, "Queued"],
    [JobRunState.RUNNING, "Running"],
    [JobRunState.COMPLETED, "Completed"],
    [JobRunState.FAILED, "Failed"],
    [JobRunState.CANCELLED, "Cancelled"],
  ];

  it.each(cases)("renders the label for state %s", (state, label) => {
    render(<JobStateBadge state={state} />);
    expect(screen.getByText(label)).toBeInTheDocument();
  });

  it("renders a neutral fallback for an unknown state", () => {
    render(<JobStateBadge state={JobRunState.UNSPECIFIED} />);
    expect(screen.getByText(/unknown/i)).toBeInTheDocument();
  });

  it("applies the destructive tint for a failed run", () => {
    const { container } = render(<JobStateBadge state={JobRunState.FAILED} />);
    const badge = container.firstChild as HTMLElement;
    expect(badge.className).toMatch(/red/);
  });

  it("applies the running (violet) tint", () => {
    const { container } = render(<JobStateBadge state={JobRunState.RUNNING} />);
    const badge = container.firstChild as HTMLElement;
    expect(badge.className).toMatch(/violet/);
  });
});
