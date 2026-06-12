import { useQuery } from "@connectrpc/connect-query";
import { Fragment } from "react";

import type { Credit } from "../gen/omnibus/v1/series_pb";
import { getIssue } from "../gen/omnibus/v1/series-SeriesService_connectquery";

// roleLabels maps the normalized Credit.role values (writer|penciller|inker|colorist|
// letterer|editor|cover) to their human display labels, in canonical comic-credit order.
// Roles are rendered in this order; any role with no credits is skipped, and any role not
// in this map (e.g. an unrecognized ComicVine role) is ignored for the labeled block.
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
// on the left and (on the right) the issue number, title, dates, creator credits grouped
// by role, issue type + page count, and the summary below. It fetches GetIssue on demand
// (the issue list stays light). Loading shows a slim placeholder; an error shows muted copy.
export function IssueDetailPanel({ issueId }: { issueId: bigint }) {
  const detail = useQuery(getIssue, { issueId }, { retry: false });

  if (detail.isError) {
    return <p className="text-sm text-slate-500">Couldn't load details</p>;
  }

  const issueDetail = detail.data?.issue;
  if (!issueDetail || !issueDetail.issue) {
    return (
      <div className="flex gap-4 rounded bg-slate-100 p-4">
        <div className="aspect-[2/3] w-40 animate-pulse rounded bg-slate-200" />
        <div className="flex flex-1 flex-col gap-2">
          <div className="h-5 w-24 animate-pulse rounded bg-slate-200" />
          <div className="h-4 w-40 animate-pulse rounded bg-slate-200" />
          <div className="h-4 w-full animate-pulse rounded bg-slate-200" />
        </div>
      </div>
    );
  }

  const { issue, description, issueType, pageCount, storeDate, credits } = issueDetail;
  const groupedCredits = groupCredits(credits);

  return (
    <div className="flex gap-6 rounded-lg bg-slate-100 p-5">
      {issue.coverUrl ? (
        <img
          src={issue.coverUrl}
          alt={`Cover for issue ${issue.issueNumber}`}
          className="aspect-[2/3] w-40 shrink-0 rounded object-cover shadow-sm"
        />
      ) : (
        <div className="flex aspect-[2/3] w-40 shrink-0 items-center justify-center rounded bg-slate-200 text-sm font-semibold text-slate-500 shadow-sm">
          #{issue.issueNumber}
        </div>
      )}

      <div className="flex flex-1 flex-col gap-4">
        <div className="flex flex-col gap-1">
          <h3 className="text-lg font-semibold text-slate-900">
            #{issue.issueNumber}
            {issue.title ? ` · ${issue.title}` : ""}
          </h3>
          {issue.coverDate || storeDate ? (
            <span className="text-sm text-slate-600">
              {[issue.coverDate, storeDate].filter(Boolean).join(" · ")}
            </span>
          ) : null}
          {issueType || pageCount > 0 ? (
            <span className="text-sm text-slate-500">
              {issueType}
              {pageCount > 0 ? ` · ${pageCount} pages` : ""}
            </span>
          ) : null}
        </div>

        <section className="flex flex-col gap-2">
          <h4 className="text-xs font-semibold uppercase tracking-wide text-slate-500">
            Contributors
          </h4>
          {groupedCredits.length > 0 ? (
            <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm sm:grid-cols-[auto_1fr_auto_1fr]">
              {groupedCredits.map((row) => (
                <Fragment key={row.label}>
                  <dt className="font-semibold text-slate-600">{row.label}</dt>
                  <dd className="text-slate-800">{row.names}</dd>
                </Fragment>
              ))}
            </dl>
          ) : (
            <p className="text-sm text-slate-500">No contributor credits</p>
          )}
        </section>

        {description ? (
          <section className="flex flex-col gap-2">
            <h4 className="text-xs font-semibold uppercase tracking-wide text-slate-500">
              Summary
            </h4>
            <p className="whitespace-pre-line text-base text-slate-700">{description}</p>
          </section>
        ) : null}
      </div>
    </div>
  );
}
