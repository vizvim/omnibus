import { ConnectError, Code } from "@connectrpc/connect";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { App } from "./App";
import { IssueStatus } from "./gen/omnibus/v1/common_pb";
import { makeTransport, renderWithProviders } from "./test/render";

const daredevil1964 = {
  id: 0n,
  comicvineVolumeId: 4050n,
  name: "Daredevil",
  startYear: 1964,
  publisher: "Marvel",
  totalIssues: 380,
  haveIssues: 0,
  coverUrl: "",
};
const daredevil2011 = {
  ...daredevil1964,
  comicvineVolumeId: 48910n,
  startYear: 2011,
  totalIssues: 36,
};

describe("search flow", () => {
  it("renders the Search ComicVine CTA and disambiguation cards", async () => {
    const transport = makeTransport({
      searchComicVine: () => ({ candidates: [daredevil1964, daredevil2011] }),
      listSeries: () => ({ series: [] }),
    });
    renderWithProviders(<App initialRoute="search" />, transport);

    expect(screen.getByRole("button", { name: /search comicvine/i })).toBeInTheDocument();

    fireEvent.change(screen.getByRole("textbox"), { target: { value: "Daredevil" } });
    fireEvent.click(screen.getByRole("button", { name: /search comicvine/i }));

    await waitFor(() => {
      expect(screen.getAllByText("Daredevil").length).toBeGreaterThanOrEqual(2);
    });
    expect(screen.getByText("1964")).toBeInTheDocument();
    expect(screen.getByText("2011")).toBeInTheDocument();
    expect(screen.getAllByText("Marvel").length).toBeGreaterThanOrEqual(2);
  });

  it("renders the no-matches copy on an empty result", async () => {
    const transport = makeTransport({
      searchComicVine: () => ({ candidates: [] }),
      listSeries: () => ({ series: [] }),
    });
    renderWithProviders(<App initialRoute="search" />, transport);

    fireEvent.change(screen.getByRole("textbox"), { target: { value: "zzzz" } });
    fireEvent.click(screen.getByRole("button", { name: /search comicvine/i }));

    await waitFor(() => {
      expect(screen.getByText(/no matches found/i)).toBeInTheDocument();
    });
  });

  it("renders the error copy with a Retry action on RPC failure", async () => {
    const transport = makeTransport({
      searchComicVine: () => {
        throw new ConnectError("boom", Code.Unavailable);
      },
      listSeries: () => ({ series: [] }),
    });
    renderWithProviders(<App initialRoute="search" />, transport);

    fireEvent.change(screen.getByRole("textbox"), { target: { value: "Daredevil" } });
    fireEvent.click(screen.getByRole("button", { name: /search comicvine/i }));

    await waitFor(() => {
      expect(screen.getAllByText(/couldn't reach comicvine/i).length).toBeGreaterThanOrEqual(1);
    });
    expect(screen.getByRole("button", { name: /retry/i })).toBeInTheDocument();
  });
});

describe("library", () => {
  it("renders the empty state when there are no series", async () => {
    const transport = makeTransport({ listSeries: () => ({ series: [] }) });
    renderWithProviders(<App initialRoute="library" />, transport);

    await waitFor(() => {
      expect(screen.getByText(/no series in your library yet/i)).toBeInTheDocument();
    });
  });

  it("renders a series grid with a syncing badge when importing", async () => {
    const importing = { ...daredevil1964, id: 1n, totalIssues: 100, haveIssues: 10 };
    const transport = makeTransport({ listSeries: () => ({ series: [importing] }) });
    renderWithProviders(<App initialRoute="library" />, transport);

    await waitFor(() => {
      expect(screen.getAllByText("Daredevil").length).toBeGreaterThanOrEqual(1);
    });
    expect(screen.getByText(/syncing/i)).toBeInTheDocument();
  });
});

describe("series detail", () => {
  it("renders the issue grid incl. non-integer numbers + status badges", async () => {
    const series = { ...daredevil1964, id: 5n, totalIssues: 3, haveIssues: 3 };
    const issues = [
      { id: 1n, seriesId: 5n, comicvineIssueId: 1n, issueNumber: "7.INH", title: "Inh", status: IssueStatus.WANTED, coverUrl: "", issueNumberSort: 7, issueNumberQualifier: "INH", coverDate: "" },
      { id: 2n, seriesId: 5n, comicvineIssueId: 2n, issueNumber: "½", title: "Half", status: IssueStatus.DOWNLOADED, coverUrl: "/covers/issues/2.jpg", issueNumberSort: 0.5, issueNumberQualifier: "half", coverDate: "" },
      { id: 3n, seriesId: 5n, comicvineIssueId: 3n, issueNumber: "Annual 1", title: "Annual", status: IssueStatus.FAILED, coverUrl: "", issueNumberSort: 1, issueNumberQualifier: "annual", coverDate: "" },
    ];
    const transport = makeTransport({
      getSeries: () => ({ series, issues, publisher: "Marvel", storyArcs: [] }),
    });
    renderWithProviders(<App initialRoute="detail" initialSeriesId={5n} />, transport);

    await waitFor(() => {
      expect(screen.getAllByText(/7\.INH/).length).toBeGreaterThanOrEqual(1);
    });
    expect(screen.getAllByText(/½/).length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText(/Annual 1/).length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText("Wanted")).toBeInTheDocument();
    expect(screen.getByText("Downloaded")).toBeInTheDocument();
    expect(screen.getByText("Failed")).toBeInTheDocument();
    // Missing cover -> placeholder, never a broken <img>: the placeholder shows the number.
    const imgs = screen.queryAllByRole("img");
    for (const img of imgs) {
      expect(img.getAttribute("src")).toContain("/covers/");
    }
  });

  it("renders a Refresh now button that enqueues a refresh when idle", async () => {
    const series = { ...daredevil1964, id: 7n, totalIssues: 2, haveIssues: 2, lastRefreshedAt: "" };
    let refreshed = 0;
    const transport = makeTransport({
      getSeries: () => ({ series, issues: [], publisher: "Marvel", storyArcs: [] }),
      refreshSeries: () => {
        refreshed += 1;
        return { series };
      },
    });
    renderWithProviders(<App initialRoute="detail" initialSeriesId={7n} />, transport);

    const button = await screen.findByRole("button", { name: /refresh now/i });
    expect(button).toBeEnabled();

    fireEvent.click(button);
    await waitFor(() => expect(refreshed).toBe(1));
  });

  it("disables Refresh now while the series is still importing", async () => {
    const importing = { ...daredevil1964, id: 8n, totalIssues: 100, haveIssues: 10, lastRefreshedAt: "" };
    const transport = makeTransport({
      getSeries: () => ({ series: importing, issues: [], publisher: "Marvel", storyArcs: [] }),
    });
    renderWithProviders(<App initialRoute="detail" initialSeriesId={8n} />, transport);

    const button = await screen.findByRole("button", { name: /refresh now/i });
    expect(button).toBeDisabled();
  });
});
