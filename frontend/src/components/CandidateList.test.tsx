import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { makeSearchTransport, renderWithProviders } from "../test/render";
import { CandidateList } from "./CandidateList";

describe("CandidateList", () => {
  it("renders ranked rows with score metadata", async () => {
    const transport = makeSearchTransport({
      searchIssue: () => ({
        acceptable: true,
        floorReason: "",
        candidates: [
          {
            provider: "newznab",
            releaseKey: "rk-1",
            title: "Saga #7 (cbz)",
            sizeBytes: 31457280n,
            score: 82,
            reason: "",
          },
        ],
      }),
    });
    renderWithProviders(<CandidateList issueId={7n} enabled />, transport);

    await waitFor(() => {
      expect(screen.getByText("Saga #7 (cbz)")).toBeInTheDocument();
    });
    // Score is rendered as text metadata, not a badge.
    expect(screen.getByText(/Score 82/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /grab/i })).toBeInTheDocument();
  });

  it("fires SelectCandidate when Grab is clicked", async () => {
    const selectSpy = vi.fn(
      (_req: { issueId: bigint; provider: string; releaseKey: string }) => ({
        download: {
          id: 1n,
          issueId: 7n,
          provider: "newznab",
          releaseKey: "rk-1",
          status: "Queued",
          clientRef: "nzo_1",
        },
      }),
    );
    const transport = makeSearchTransport({
      searchIssue: () => ({
        acceptable: true,
        floorReason: "",
        candidates: [
          {
            provider: "newznab",
            releaseKey: "rk-1",
            title: "Saga #7 (cbz)",
            sizeBytes: 31457280n,
            score: 82,
            reason: "",
          },
        ],
      }),
      selectCandidate: selectSpy,
    });
    renderWithProviders(<CandidateList issueId={7n} enabled />, transport);

    await waitFor(() => {
      expect(screen.getByRole("button", { name: /grab/i })).toBeInTheDocument();
    });
    fireEvent.click(screen.getByRole("button", { name: /grab/i }));

    await waitFor(() => {
      expect(selectSpy).toHaveBeenCalledTimes(1);
    });
    const req = selectSpy.mock.calls[0][0];
    expect(req.issueId).toBe(7n);
    expect(req.releaseKey).toBe("rk-1");
  });

  it("shows the no-acceptable empty state with the floor reason", async () => {
    const transport = makeSearchTransport({
      searchIssue: () => ({
        acceptable: false,
        floorReason: "best candidate scored 40 < floor 60",
        candidates: [],
      }),
    });
    renderWithProviders(<CandidateList issueId={7n} enabled />, transport);

    await waitFor(() => {
      expect(screen.getByText(/no acceptable releases/i)).toBeInTheDocument();
    });
    expect(screen.getByText(/best candidate scored 40 < floor 60/)).toBeInTheDocument();
  });
});
