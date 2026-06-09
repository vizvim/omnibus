import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { App } from "../App";
import { makeIndexerTransport, renderWithProviders } from "../test/render";

describe("indexers page", () => {
  it("renders the empty state when there are no indexers", async () => {
    const transport = makeIndexerTransport({ listIndexers: () => ({ indexers: [] }) });
    renderWithProviders(<App initialRoute="indexers" />, transport);

    await waitFor(() => {
      expect(screen.getByText(/no indexers configured/i)).toBeInTheDocument();
    });
  });

  it("renders the API key input as a password field in the add form", async () => {
    const transport = makeIndexerTransport({ listIndexers: () => ({ indexers: [] }) });
    renderWithProviders(<App initialRoute="indexers" />, transport);

    await waitFor(() => {
      expect(screen.getByText(/no indexers configured/i)).toBeInTheDocument();
    });

    // Open the add form (there are two "Add indexer" buttons — header + empty-state CTA).
    fireEvent.click(screen.getAllByRole("button", { name: /add indexer/i })[0]);

    const apiKey = screen.getByLabelText(/api key/i);
    expect(apiKey).toHaveAttribute("type", "password");
  });

  it("lists indexers with an enabled badge", async () => {
    const transport = makeIndexerTransport({
      listIndexers: () => ({
        indexers: [
          {
            id: 1n,
            name: "My Newznab",
            kind: "newznab",
            baseUrl: "http://nzb.test",
            enabled: true,
            categories: "7030",
            priority: 0,
            useForRss: true,
            createdAt: "2026-01-01T00:00:00Z",
            updatedAt: "2026-01-01T00:00:00Z",
          },
        ],
      }),
    });
    renderWithProviders(<App initialRoute="indexers" />, transport);

    await waitFor(() => {
      expect(screen.getByText("My Newznab")).toBeInTheDocument();
    });
    expect(screen.getByText("Enabled")).toBeInTheDocument();
  });
});
