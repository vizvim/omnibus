import type { Issue } from "../gen/omnibus/v1/series_pb";
import { StatusBadge } from "./StatusBadge";

// IssueTile shows one issue: its cover (served from the backend /covers route) or a neutral
// placeholder with the issue number when no cover is stored — never a broken image
// (D-07/D-08). It shows the (possibly non-integer) issue number, title, and status.
export function IssueTile({ issue }: { issue: Issue }) {
  return (
    <div className="group flex flex-col overflow-hidden rounded-lg border border-border bg-card/70 transition-colors duration-200 group-aria-pressed:border-primary">
      <div className="relative aspect-[2/3] w-full overflow-hidden bg-secondary">
        {issue.coverUrl ? (
          <img
            src={issue.coverUrl}
            alt={`Cover for issue ${issue.issueNumber}`}
            className="size-full object-cover transition-transform duration-300 group-hover:scale-[1.05]"
          />
        ) : (
          <div className="flex size-full items-center justify-center font-mono text-base font-semibold text-muted-foreground">
            #{issue.issueNumber}
          </div>
        )}
      </div>
      <div className="flex flex-col gap-1 p-2">
        <div className="flex items-center justify-between gap-1">
          <span className="font-mono text-xs font-semibold text-foreground">
            #{issue.issueNumber}
          </span>
          <StatusBadge status={issue.status} />
        </div>
        {issue.title ? (
          <span className="truncate text-xs text-muted-foreground">{issue.title}</span>
        ) : null}
      </div>
    </div>
  );
}
