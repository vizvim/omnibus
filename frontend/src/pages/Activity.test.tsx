import { ConnectError, Code } from "@connectrpc/connect";
import { screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { App } from "../App";
import { JobRunState } from "../gen/omnibus/v1/jobs_pb";
import { makeJobTransport, renderWithProviders } from "../test/render";

describe("activity page", () => {
  it("renders the empty state when there are no job runs", async () => {
    const transport = makeJobTransport({ listJobRuns: () => ({ runs: [] }) });
    renderWithProviders(<App initialRoute="activity" />, transport);

    await waitFor(() => {
      expect(screen.getByText(/no job runs yet/i)).toBeInTheDocument();
    });
  });

  it("renders a populated list with a state badge and a failed-run error", async () => {
    const transport = makeJobTransport({
      listJobRuns: () => ({
        runs: [
          {
            id: "1",
            kind: "import_series",
            state: JobRunState.COMPLETED,
            startedAt: "2026-01-01T00:00:00Z",
            finishedAt: "2026-01-01T00:01:00Z",
            error: "",
            attempt: 1,
          },
          {
            id: "2",
            kind: "refresh_series",
            state: JobRunState.FAILED,
            startedAt: "2026-01-02T00:00:00Z",
            finishedAt: "2026-01-02T00:02:00Z",
            error: "comicvine 500",
            attempt: 3,
          },
        ],
      }),
    });
    renderWithProviders(<App initialRoute="activity" />, transport);

    await waitFor(() => {
      expect(screen.getByText("Import series")).toBeInTheDocument();
    });
    expect(screen.getByText("Refresh series")).toBeInTheDocument();
    expect(screen.getByText("Completed")).toBeInTheDocument();
    expect(screen.getByText("Failed")).toBeInTheDocument();
    // The failed run surfaces its error string.
    expect(screen.getByText("comicvine 500")).toBeInTheDocument();
  });

  it("renders the error state when the RPC fails", async () => {
    const transport = makeJobTransport({
      listJobRuns: () => {
        throw new ConnectError("boom", Code.Unavailable);
      },
    });
    renderWithProviders(<App initialRoute="activity" />, transport);

    await waitFor(() => {
      expect(screen.getByText(/couldn't load activity/i)).toBeInTheDocument();
    });
  });
});
