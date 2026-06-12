import { screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { IssueStatus } from "../gen/omnibus/v1/common_pb";
import { makeTransport, renderWithProviders } from "../test/render";
import { IssueDetailPanel } from "./IssueDetailPanel";

describe("IssueDetailPanel", () => {
  it("renders the summary, grouped credits by role, and the page count", async () => {
    const transport = makeTransport({
      getIssue: () => ({
        issue: {
          issue: {
            id: 7n,
            seriesId: 1n,
            comicvineId: 100n,
            issueNumber: "7",
            issueNumberQualifier: "",
            title: "The Will",
            coverDate: "2013-08-14",
            status: IssueStatus.WANTED,
            coverUrl: "/covers/issues/7",
            issueType: "standard",
          },
          description: "Marko and Alana flee across the galaxy.",
          issueType: "standard",
          altIssueNumber: "",
          pageCount: 24,
          storeDate: "2013-08-14",
          cvLastUpdated: "",
          // Returned ordered by (role, name); two writers should join with ", ".
          credits: [
            { role: "writer", name: "Brian K. Vaughan", comicvinePersonId: 1n },
            { role: "writer", name: "Co Writer", comicvinePersonId: 2n },
            { role: "penciller", name: "Fiona Staples", comicvinePersonId: 3n },
            { role: "cover", name: "Fiona Staples", comicvinePersonId: 3n },
          ],
        },
      }),
    });
    renderWithProviders(<IssueDetailPanel issueId={7n} />, transport);

    await waitFor(() => {
      expect(
        screen.getByText("Marko and Alana flee across the galaxy."),
      ).toBeInTheDocument();
    });

    // Credits grouped by role, multiple names joined with ", ".
    expect(screen.getByText("Writer")).toBeInTheDocument();
    expect(screen.getByText("Brian K. Vaughan, Co Writer")).toBeInTheDocument();
    expect(screen.getByText("Penciller")).toBeInTheDocument();
    // "cover" role maps to the "Cover Artist" display label.
    expect(screen.getByText("Cover Artist")).toBeInTheDocument();
    // Both Penciller and Cover Artist credit Fiona Staples.
    expect(screen.getAllByText("Fiona Staples").length).toBe(2);
    // The Inker/Colorist/Letterer/Editor roles have no credits and are dropped.
    expect(screen.queryByText("Inker")).not.toBeInTheDocument();
    expect(screen.queryByText("Editor")).not.toBeInTheDocument();
    // With credits present, the explicit empty state is NOT shown.
    expect(screen.queryByText("No contributor credits")).not.toBeInTheDocument();

    // Meta line: issue type + "· N pages".
    expect(screen.getByText(/· 24 pages/)).toBeInTheDocument();

    // The summary renders as plain text, not parsed HTML. A description containing
    // markup-looking text must surface verbatim and never inject an element.
    const summary = screen.getByText("Marko and Alana flee across the galaxy.");
    expect(summary.tagName.toLowerCase()).toBe("p");
    expect(summary.querySelector("*")).toBeNull();
  });

  it("renders summary text literally without interpreting HTML", async () => {
    const transport = makeTransport({
      getIssue: () => ({
        issue: {
          issue: {
            id: 11n,
            seriesId: 1n,
            comicvineId: 300n,
            issueNumber: "11",
            issueNumberQualifier: "",
            title: "Markup",
            coverDate: "",
            status: IssueStatus.WANTED,
            coverUrl: "",
            issueType: "",
          },
          // Backend now strips HTML, but if any angle-bracket text slips through it must
          // render as literal characters, never as a DOM element.
          description: "A <b>bold</b> tale unfolds.",
          issueType: "",
          altIssueNumber: "",
          pageCount: 0,
          storeDate: "",
          cvLastUpdated: "",
          credits: [],
        },
      }),
    });
    const { container } = renderWithProviders(
      <IssueDetailPanel issueId={11n} />,
      transport,
    );

    await waitFor(() => {
      expect(
        screen.getByText("A <b>bold</b> tale unfolds."),
      ).toBeInTheDocument();
    });
    // No <b> element was injected — the text is literal.
    expect(container.querySelector("b")).toBeNull();
  });

  it("omits the credits, page count, and summary blocks when absent", async () => {
    const transport = makeTransport({
      getIssue: () => ({
        issue: {
          issue: {
            id: 9n,
            seriesId: 1n,
            comicvineId: 200n,
            issueNumber: "9",
            issueNumberQualifier: "",
            title: "",
            coverDate: "",
            status: IssueStatus.WANTED,
            coverUrl: "",
            issueType: "",
          },
          description: "",
          issueType: "",
          altIssueNumber: "",
          pageCount: 0,
          storeDate: "",
          cvLastUpdated: "",
          credits: [],
        },
      }),
    });
    const { container } = renderWithProviders(
      <IssueDetailPanel issueId={9n} />,
      transport,
    );

    // The header renders once loaded. The issue number is split across text nodes
    // (literal "#" + the interpolated number), so match on the element's textContent.
    await waitFor(() => {
      expect(
        screen.getAllByText(
          (_, el) => el?.tagName.toLowerCase() === "h3" && el.textContent === "#9",
        ).length,
      ).toBeGreaterThan(0);
    });

    // No credit role labels and no "pages" meta line.
    expect(screen.queryByText("Writer")).not.toBeInTheDocument();
    expect(screen.queryByText(/pages/)).not.toBeInTheDocument();

    // The empty-credits state is explicit (a muted line), not a blank gap.
    expect(screen.getByText("No contributor credits")).toBeInTheDocument();

    // The summary section is omitted entirely when the description is empty: the only
    // <p> present is the "No contributor credits" line.
    const paragraphs = container.querySelectorAll("p");
    expect(paragraphs.length).toBe(1);
    expect(paragraphs[0]?.textContent).toBe("No contributor credits");
  });
});
