import { useMutation, useQuery } from "@connectrpc/connect-query";
import { Fragment, useState } from "react";

import { CandidateList } from "../components/CandidateList";
import { EmptyState } from "../components/EmptyState";
import { IssueDetailPanel } from "../components/IssueDetailPanel";
import { IssueTimeline } from "../components/IssueTimeline";
import { IssueTile } from "../components/IssueTile";
import { SyncingBadge } from "../components/SyncingBadge";
import { triggerAutoSearch } from "../gen/omnibus/v1/search-SearchService_connectquery";
import {
  getSeries,
  refreshSeries,
} from "../gen/omnibus/v1/series-SeriesService_connectquery";
import { relativeTime } from "../lib/time";

// SeriesDetail renders the GetSeries header + issue grid. While the series is still
// importing (have < total) it polls GetSeries on an interval so issues + covers appear
// as they land, then stops polling once caught up (D-06).
export function SeriesDetail({ seriesId }: { seriesId: bigint }) {
  // The selected issue exposes the per-issue Candidates + Timeline panels. searchActive
  // gates the SearchIssue query so it only runs after the user clicks "Search".
  const [selectedIssueId, setSelectedIssueId] = useState<bigint | null>(null);
  const [searchActive, setSearchActive] = useState(false);

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

  // The "Auto-grab & download" path: run the inline search-and-auto-grab (bypassing the
  // River queue) and report the outcome. On success re-poll GetSeries so the issue's
  // status (e.g. Snatched) updates; the polled IssueTimeline surfaces the snatched event.
  const autoGrab = useMutation(triggerAutoSearch, {
    onSuccess: () => void detail.refetch(),
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
            <Fragment key={issue.id.toString()}>
              <button
                type="button"
                onClick={() => {
                  setSelectedIssueId(issue.id);
                  setSearchActive(false);
                  autoGrab.reset();
                }}
                aria-pressed={selectedIssueId === issue.id}
                className={`rounded text-left ${
                  selectedIssueId === issue.id ? "ring-2 ring-blue-600" : ""
                }`}
              >
                <IssueTile issue={issue} />
              </button>

              {selectedIssueId === issue.id ? (
                // Expand the selected-issue detail inline, full-width, directly under the
                // clicked tile. col-span-full makes it occupy a whole grid row.
                <section className="col-span-full flex flex-col gap-6">
                  <IssueDetailPanel issueId={issue.id} />

                  <section className="flex flex-col gap-4">
                    <div className="flex items-center gap-4">
                      <h2 className="text-xl font-semibold">Candidates</h2>
                      <button
                        type="button"
                        disabled={autoGrab.isPending}
                        onClick={() => autoGrab.mutate({ issueId: issue.id })}
                        className="rounded bg-blue-600 px-4 py-2 text-sm font-semibold text-white disabled:cursor-not-allowed disabled:bg-slate-400"
                      >
                        {autoGrab.isPending ? "Auto-grabbing…" : "Auto-grab & download"}
                      </button>
                      <button
                        type="button"
                        onClick={() => setSearchActive(true)}
                        className="rounded border border-slate-300 px-4 py-2 text-sm font-semibold text-slate-700"
                      >
                        Search & choose
                      </button>
                    </div>
                    {autoGrab.isError ? (
                      <p className="text-sm text-red-600">
                        Couldn't complete the auto-grab. Check the server and download client
                        are reachable, then try again.
                      </p>
                    ) : autoGrab.data ? (
                      autoGrab.data.grabbed ? (
                        <p className="text-sm text-green-700">
                          Grabbed {autoGrab.data.title}. Handed off to the download client —
                          see the timeline below.
                        </p>
                      ) : (
                        <p className="text-sm text-slate-600">
                          {autoGrab.data.floorReason
                            ? `No release cleared the match and quality checks (${autoGrab.data.floorReason}). omnibus will keep trying automatically, or use “Search & choose” to grab one manually.`
                            : "No release cleared the match and quality checks. omnibus will keep trying automatically, or use “Search & choose” to grab one manually."}
                        </p>
                      )
                    ) : null}
                    <CandidateList
                      issueId={issue.id}
                      enabled={searchActive}
                      onGrabbed={() => void detail.refetch()}
                    />
                  </section>

                  <section className="flex flex-col gap-4">
                    <h2 className="text-xl font-semibold">Timeline</h2>
                    <IssueTimeline issueId={issue.id} />
                  </section>
                </section>
              ) : null}
            </Fragment>
          ))}
        </div>
      </section>
    </div>
  );
}
