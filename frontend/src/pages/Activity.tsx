import { useQuery } from "@connectrpc/connect-query";

import { EmptyState } from "../components/EmptyState";
import { JobStateBadge } from "../components/JobStateBadge";
import { JobRunState } from "../gen/omnibus/v1/jobs_pb";
import { listJobRuns } from "../gen/omnibus/v1/jobs-JobService_connectquery";

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

// Activity is the functional-minimal job-run history page (D-08.2): a single-column
// list of recent runs (kind, state badge, timestamps, error). It reuses EmptyState for
// the empty + error cases and polls only while a run is still running.
export function Activity() {
  const runs = useQuery(
    listJobRuns,
    { limit: 50 },
    {
      retry: false,
      refetchInterval: (query) => {
        const list = query.state.data?.runs ?? [];
        const anyRunning = list.some((r) => r.state === JobRunState.RUNNING);
        return anyRunning ? 2000 : false;
      },
    },
  );

  if (runs.isError) {
    return (
      <EmptyState
        heading="Couldn't load activity"
        body="Couldn't reach the server. Check your connection, then try again."
      />
    );
  }

  const list = runs.data?.runs ?? [];

  return (
    <div className="flex flex-col gap-6">
      <h1 className="text-2xl font-semibold">Activity</h1>
      <section className="flex flex-col gap-4">
        <h2 className="text-xl font-semibold">Recent job runs</h2>
        {list.length === 0 ? (
          <EmptyState
            heading="No job runs yet"
            body="Background jobs like metadata refresh will appear here once they run. Add or refresh a series to kick one off."
          />
        ) : (
          <ul className="flex flex-col gap-2">
            {list.map((run) => (
              <li
                key={run.id}
                className="flex flex-col gap-1 rounded border border-slate-200 bg-slate-50 p-4"
              >
                <div className="flex items-center gap-4">
                  <span className="text-base">{kindLabel(run.kind)}</span>
                  <JobStateBadge state={run.state} />
                </div>
                <span className="text-sm text-slate-600">
                  {run.startedAt ? `Started ${run.startedAt}` : "Not started"}
                  {run.finishedAt ? ` · Finished ${run.finishedAt}` : ""}
                </span>
                {run.state === JobRunState.FAILED && run.error ? (
                  <span className="truncate text-sm text-red-600" title={run.error}>
                    {run.error}
                  </span>
                ) : null}
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}
