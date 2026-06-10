import { screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { IssueEventType } from "../gen/omnibus/v1/common_pb";
import { makeSearchTransport, renderWithProviders } from "../test/render";
import { IssueTimeline } from "./IssueTimeline";

describe("IssueTimeline", () => {
  it("renders events most-recent first with the correct tint per type", async () => {
    const transport = makeSearchTransport({
      // Service returns events occurred_at ascending: searched (oldest) -> snatched.
      getIssueTimeline: () => ({
        events: [
          {
            id: 1n,
            issueId: 7n,
            type: IssueEventType.SEARCHED,
            occurredAt: "2026-01-01T00:00:00Z",
            detail: "",
          },
          {
            id: 2n,
            issueId: 7n,
            type: IssueEventType.CANDIDATE_SELECTED,
            occurredAt: "2026-01-01T00:01:00Z",
            detail: "",
          },
          {
            id: 3n,
            issueId: 7n,
            type: IssueEventType.SNATCHED,
            occurredAt: "2026-01-01T00:02:00Z",
            detail: "",
          },
        ],
      }),
    });
    const { container } = renderWithProviders(<IssueTimeline issueId={7n} />, transport);

    await waitFor(() => {
      expect(container.querySelectorAll("span.rounded.px-2").length).toBe(3);
    });

    // Badge pills, in DOM order. Most-recent (Snatched) first, oldest (Searched) last.
    const badges = Array.from(
      container.querySelectorAll<HTMLElement>("span.rounded.px-2"),
    );
    const badgeLabels = badges.map((b) => b.textContent);
    expect(badgeLabels).toEqual(["Snatched", "Candidate selected", "Searched"]);

    // Tint vocabulary: snatched = violet, candidate-selected = green, searched = slate.
    expect(badges[0].className).toMatch(/violet/);
    expect(badges[1].className).toMatch(/green/);
    expect(badges[2].className).toMatch(/slate/);
  });

  it("shows the empty state when the issue has no events", async () => {
    const transport = makeSearchTransport({
      getIssueTimeline: () => ({ events: [] }),
    });
    renderWithProviders(<IssueTimeline issueId={7n} />, transport);

    await waitFor(() => {
      expect(screen.getByText(/nothing here yet/i)).toBeInTheDocument();
    });
  });
});
