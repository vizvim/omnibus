import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { IssueStatus } from "../gen/omnibus/v1/common_pb";
import { StatusBadge } from "./StatusBadge";

describe("StatusBadge", () => {
  const cases: [IssueStatus, string][] = [
    [IssueStatus.WANTED, "Wanted"],
    [IssueStatus.SNATCHED, "Snatched"],
    [IssueStatus.DOWNLOADED, "Downloaded"],
    [IssueStatus.ARCHIVED, "Archived"],
    [IssueStatus.SKIPPED, "Skipped"],
    [IssueStatus.FAILED, "Failed"],
    [IssueStatus.IGNORED, "Ignored"],
  ];

  it.each(cases)("renders the label for status %s", (status, label) => {
    render(<StatusBadge status={status} />);
    expect(screen.getByText(label)).toBeInTheDocument();
  });

  it("renders a neutral fallback for an unknown status", () => {
    render(<StatusBadge status={IssueStatus.UNSPECIFIED} />);
    // Fallback shows a neutral label, not a crash.
    expect(screen.getByText(/unknown/i)).toBeInTheDocument();
  });

  it("applies a tint class per status", () => {
    const { container } = render(<StatusBadge status={IssueStatus.FAILED} />);
    const badge = container.firstChild as HTMLElement;
    // Failed reuses the destructive (red) tint.
    expect(badge.className).toMatch(/red/);
  });
});
