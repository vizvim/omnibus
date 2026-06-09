import { useQuery } from "@connectrpc/connect-query";

import { EmptyState } from "../components/EmptyState";
import { IssueTile } from "../components/IssueTile";
import { SyncingBadge } from "../components/SyncingBadge";
import { getSeries } from "../gen/omnibus/v1/series-SeriesService_connectquery";

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

  return (
    <div className="flex flex-col gap-6">
      <header className="flex flex-col gap-1">
        <div className="flex items-center gap-2">
          <h1 className="text-2xl font-semibold">{series.name}</h1>
          {importing ? <SyncingBadge /> : null}
        </div>
        {publisher ? <span className="text-base text-slate-600">{publisher}</span> : null}
        <span className="text-sm text-slate-600">
          {series.haveIssues}/{series.totalIssues} issues
        </span>
        {importing ? (
          <span className="text-sm text-slate-600">
            Syncing issues from ComicVine… this updates automatically.
          </span>
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
