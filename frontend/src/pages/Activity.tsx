import { useQuery } from "@connectrpc/connect-query";
import { Waves, WifiOff } from "lucide-react";

import { EmptyState } from "../components/EmptyState";
import { JobStateBadge } from "../components/JobStateBadge";
import { PageHeader } from "../components/layout/PageHeader";
import { JobRunState } from "../gen/omnibus/v1/jobs_pb";
import { listJobRuns } from "../gen/omnibus/v1/jobs-JobService_connectquery";
import { relativeTime } from "../lib/time";
import { useConnectionState } from "../lib/transport";

// kindLabel maps the engine job kind to a human label.
function kindLabel(kind: string): string {
  switch (kind) {
    case "import_series":
      return "Import series";
    case "refresh_series":
      return "Refresh series";
    case "refresh_sweep":
      return "Scheduled refresh sweep";
    default:
      return kind;
  }
}

// timeLabel renders a short started/finished line, preferring relative time but keeping the
// raw value as a tooltip.
function timeLabel(startedAt: string, finishedAt: string): string {
  const started = startedAt ? relativeTime(startedAt) || startedAt : "";
  const parts: string[] = [];
  parts.push(started ? `Started ${started}` : "Not started");
  if (finishedAt) {
    parts.push(`Finished ${relativeTime(finishedAt) || finishedAt}`);
  }
  return parts.join(" · ");
}

// Activity is the job-run history page (D-08.2): a single-column list of recent runs (kind,
// state badge, timestamps, error). The live stream now drives updates — JobStateEvents
// invalidate this query so runs update without polling (D-08/D-10). Polling is demoted to an
// offline-only fallback: while the stream is live no periodic GET fires; only when the stream
// is disconnected does it fall back to polling running jobs.
export function Activity() {
  const connection = useConnectionState();
  const runs = useQuery(
    listJobRuns,
    { limit: 50 },
    {
      retry: false,
      refetchInterval: (query) => {
        // Stream drives updates while connected; only fall back to polling when offline (D-10).
        if (connection !== "offline") {
          return false;
        }
        const list = query.state.data?.runs ?? [];
        const anyRunning = list.some((r) => r.state === JobRunState.RUNNING);
        return anyRunning ? 2000 : false;
      },
    },
  );

  if (runs.isError) {
    return (
      <div className="flex flex-col gap-8">
        <PageHeader eyebrow="Background jobs" title="Activity" />
        <EmptyState
          icon={<WifiOff />}
          heading="Couldn't load activity"
          body="Couldn't reach the server. Check your connection, then try again."
        />
      </div>
    );
  }

  const list = runs.data?.runs ?? [];

  return (
    <div className="flex flex-col gap-8">
      <PageHeader
        eyebrow="Background jobs"
        title="Activity"
        description="Imports, metadata refreshes, and scheduled sweeps run here."
      />

      {list.length === 0 ? (
        <EmptyState
          icon={<Waves />}
          heading="No job runs yet"
          body="Background jobs like metadata refresh will appear here once they run. Add or refresh a series to kick one off."
        />
      ) : (
        <ul className="flex flex-col gap-2">
          {list.map((run) => (
            <li
              key={run.id}
              className="flex flex-col gap-2 rounded-xl border border-border bg-card/60 p-4 transition-colors hover:border-border-strong/70"
            >
              <div className="flex items-center gap-3">
                <span className="text-sm font-medium text-foreground">{kindLabel(run.kind)}</span>
                <JobStateBadge state={run.state} />
                <span className="ml-auto font-mono text-xs text-muted-foreground">
                  {timeLabel(run.startedAt, run.finishedAt)}
                </span>
              </div>
              {run.state === JobRunState.FAILED && run.error ? (
                <span
                  className="truncate rounded-md border border-tn-red/25 bg-tn-red/10 px-2.5 py-1.5 font-mono text-xs text-tn-red"
                  title={run.error}
                >
                  {run.error}
                </span>
              ) : null}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
