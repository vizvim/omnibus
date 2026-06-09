import { useMutation, useQuery } from "@connectrpc/connect-query";

import { EmptyState } from "../components/EmptyState";
import { IssueTile } from "../components/IssueTile";
import { SyncingBadge } from "../components/SyncingBadge";
import {
  getSeries,
  refreshSeries,
} from "../gen/omnibus/v1/series-SeriesService_connectquery";

// relativeTime formats an ISO timestamp as a short "x ago" string. Minimal by design —
// no date library (UI-SPEC: no new dependencies). Returns "" for an empty/invalid input.
function relativeTime(iso: string): string {
  if (!iso) return "";
  const then = Date.parse(iso);
  if (Number.isNaN(then)) return "";
  const seconds = Math.max(0, Math.round((Date.now() - then) / 1000));
  if (seconds < 60) return "just now";
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.round(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.round(hours / 24);
  return `${days}d ago`;
}

// SeriesDetail renders the GetSeries header + issue grid. While the series is still
// importing (have < total) it polls GetSeries on an interval so issues + covers appear
// as they land, then stops polling once caught up (D-06).
export function SeriesDetail({ seriesId }: { seriesId: bigint }) {
  const detail = useQuery(
    getSeries,
    { seriesId },
    {
      retry: false,
      refetchInterval: (query) => {
        const data = query.state.data;
        const series = data?.series;
        if (series && series.haveIssues < series.totalIssues) {
          return 2000; // keep polling while syncing
        }
        return false; // caught up — stop polling
      },
    },
  );

  const refresh = useMutation(refreshSeries, {
    onSuccess: () => {
      // Re-poll GetSeries so the refreshing/idle state reflects the new run.
      void detail.refetch();
    },
  });

  if (detail.isError) {
    return (
      <EmptyState
        heading="Couldn't load this series"
        body="Couldn't reach ComicVine. Check your connection and API key, then try again."
      />
    );
  }
  if (!detail.data?.series) {
    return <p className="py-12 text-center text-base text-slate-600">Loading…</p>;
  }

  const { series, issues, publisher, storyArcs } = detail.data;
  const importing = series.haveIssues < series.totalIssues;
  // A refresh is "in flight" while the enqueue mutation is pending or the series is
  // still importing (the GetSeries poll flips this back to idle once caught up).
  const refreshing = refresh.isPending || importing;
  const lastRefreshed = relativeTime(series.lastRefreshedAt);

  return (
    <div className="flex flex-col gap-6">
      <header className="flex flex-col gap-1">
        <div className="flex items-center gap-2">
          <h1 className="text-2xl font-semibold">{series.name}</h1>
          {refreshing ? <SyncingBadge /> : null}
          <button
            type="button"
            disabled={refreshing}
            onClick={() => refresh.mutate({ seriesId: series.id })}
            className="text-sm font-semibold text-blue-600 disabled:cursor-not-allowed disabled:text-slate-400"
          >
            Refresh now
          </button>
        </div>
        {publisher ? <span className="text-base text-slate-600">{publisher}</span> : null}
        <span className="text-sm text-slate-600">
          {series.haveIssues}/{series.totalIssues} issues
        </span>
        {importing ? (
          <span className="text-sm text-slate-600">
            Syncing issues from ComicVine… this updates automatically.
          </span>
        ) : refresh.isPending ? (
          <span className="text-sm text-slate-600">
            Refreshing metadata from ComicVine… this updates automatically.
          </span>
        ) : null}
        {refresh.isError ? (
          <span className="text-sm text-red-600">
            Couldn't start a refresh. Try again in a moment.
          </span>
        ) : null}
        {!refreshing && lastRefreshed ? (
          <span className="text-sm text-slate-600">Last refreshed {lastRefreshed}</span>
        ) : null}
        {storyArcs.length > 0 ? (
          <span className="text-sm text-slate-600">
            Story arcs: {storyArcs.map((a) => a.name).join(", ")}
          </span>
        ) : null}
      </header>

      <section className="flex flex-col gap-4">
        <h2 className="text-xl font-semibold">Issues</h2>
        <div className="grid grid-cols-[repeat(auto-fill,minmax(160px,1fr))] gap-4">
          {issues.map((issue) => (
            <IssueTile key={issue.id.toString()} issue={issue} />
          ))}
        </div>
      </section>
    </div>
  );
}
