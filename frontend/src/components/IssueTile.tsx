import type { Issue } from "../gen/omnibus/v1/series_pb";
import { StatusBadge } from "./StatusBadge";

// IssueTile shows one issue: its cover (served from the backend /covers route) or a
// neutral placeholder with the issue number when no cover is stored — never a broken
// image (D-07/D-08). It shows the (possibly non-integer) issue number, title, and status.
export function IssueTile({ issue }: { issue: Issue }) {
  return (
    <div className="flex flex-col gap-1 rounded bg-slate-100 p-2">
      {issue.coverUrl ? (
        <img
          src={issue.coverUrl}
          alt={`Cover for issue ${issue.issueNumber}`}
          className="aspect-[2/3] w-full rounded object-cover"
        />
      ) : (
        <div className="flex aspect-[2/3] w-full items-center justify-center rounded bg-slate-200 text-sm font-semibold text-slate-500">
          #{issue.issueNumber}
        </div>
      )}
      <div className="flex items-center justify-between gap-1">
        <span className="text-sm font-semibold">#{issue.issueNumber}</span>
        <StatusBadge status={issue.status} />
      </div>
      {issue.title ? <span className="truncate text-base">{issue.title}</span> : null}
    </div>
  );
}
