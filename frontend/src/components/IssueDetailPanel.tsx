import { useQuery } from "@connectrpc/connect-query";
import { Fragment } from "react";

import type { Credit } from "../gen/omnibus/v1/series_pb";
import { getIssue } from "../gen/omnibus/v1/series-SeriesService_connectquery";
import { Skeleton } from "./ui/skeleton";

// roleLabels maps the normalized Credit.role values to their display labels, in canonical
// comic-credit order. Roles render in this order; any role with no credits is skipped, and
// any role not in this map is ignored for the labeled block.
const roleLabels: Array<{ role: string; label: string }> = [
  { role: "writer", label: "Writer" },
  { role: "penciller", label: "Penciller" },
  { role: "inker", label: "Inker" },
  { role: "colorist", label: "Colorist" },
  { role: "letterer", label: "Letterer" },
  { role: "editor", label: "Editor" },
  { role: "cover", label: "Cover Artist" },
];

// groupCredits joins the names for each role with ", " and returns the labeled rows in
// canonical order, dropping any role with no credits.
function groupCredits(credits: Credit[]): Array<{ label: string; names: string }> {
  return roleLabels
    .map(({ role, label }) => {
      const names = credits
        .filter((c) => c.role === role)
        .map((c) => c.name)
        .join(", ");
      return { label, names };
    })
    .filter((row) => row.names.length > 0);
}

// IssueDetailPanel renders the rich ComicInfo-style card for one selected issue: the cover
// on the left and (on the right) the issue number, title, dates, creator credits grouped by
// role, issue type + page count, and the summary below. It fetches GetIssue on demand.
export function IssueDetailPanel({ issueId }: { issueId: bigint }) {
  const detail = useQuery(getIssue, { issueId }, { retry: false });

  if (detail.isError) {
    return <p className="text-sm text-muted-foreground">Couldn't load details</p>;
  }

  const issueDetail = detail.data?.issue;
  if (!issueDetail || !issueDetail.issue) {
    return (
      <div className="flex gap-6 rounded-xl border border-border bg-card/60 p-5">
        <Skeleton className="aspect-[2/3] w-40 shrink-0 rounded-lg" />
        <div className="flex flex-1 flex-col gap-3 pt-1">
          <Skeleton className="h-5 w-28" />
          <Skeleton className="h-4 w-44" />
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-4 w-3/4" />
        </div>
      </div>
    );
  }

  const { issue, description, issueType, pageCount, storeDate, credits } = issueDetail;
  const groupedCredits = groupCredits(credits);

  return (
    <div className="surface-sheen flex flex-col gap-6 rounded-xl border border-border bg-card/70 p-5 sm:flex-row">
      {issue.coverUrl ? (
        <img
          src={issue.coverUrl}
          alt={`Cover for issue ${issue.issueNumber}`}
          className="aspect-[2/3] w-40 shrink-0 self-start rounded-lg object-cover shadow-xl shadow-black/40 ring-1 ring-border"
        />
      ) : (
        <div className="flex aspect-[2/3] w-40 shrink-0 items-center justify-center self-start rounded-lg border border-border bg-secondary font-mono text-sm font-semibold text-muted-foreground">
          #{issue.issueNumber}
        </div>
      )}

      <div className="flex flex-1 flex-col gap-4">
        <div className="flex flex-col gap-1.5">
          <h3 className="font-display text-xl font-semibold tracking-tight text-foreground">
            #{issue.issueNumber}
            {issue.title ? ` · ${issue.title}` : ""}
          </h3>
          {issue.coverDate || storeDate ? (
            <span className="font-mono text-xs text-muted-foreground">
              {[issue.coverDate, storeDate].filter(Boolean).join(" · ")}
            </span>
          ) : null}
          {issueType || pageCount > 0 ? (
            <span className="text-xs text-muted-foreground/80">
              {issueType}
              {pageCount > 0 ? ` · ${pageCount} pages` : ""}
            </span>
          ) : null}
        </div>

        <section className="flex flex-col gap-2">
          <h4 className="font-mono text-[0.65rem] font-semibold uppercase tracking-wider text-primary/80">
            Contributors
          </h4>
          {groupedCredits.length > 0 ? (
            <dl className="grid grid-cols-[auto_1fr] gap-x-5 gap-y-1.5 text-sm sm:grid-cols-[auto_1fr_auto_1fr]">
              {groupedCredits.map((row) => (
                <Fragment key={row.label}>
                  <dt className="font-medium text-muted-foreground">{row.label}</dt>
                  <dd className="text-foreground">{row.names}</dd>
                </Fragment>
              ))}
            </dl>
          ) : (
            <p className="text-sm text-muted-foreground">No contributor credits</p>
          )}
        </section>

        {description ? (
          <section className="flex flex-col gap-2 border-t border-border pt-4">
            <h4 className="font-mono text-[0.65rem] font-semibold uppercase tracking-wider text-primary/80">
              Summary
            </h4>
            <p className="whitespace-pre-line text-sm leading-relaxed text-foreground/85">
              {description}
            </p>
          </section>
        ) : null}
      </div>
    </div>
  );
}
