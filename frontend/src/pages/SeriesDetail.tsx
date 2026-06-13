import { useMutation, useQuery } from "@connectrpc/connect-query";
import { ArrowLeft, Download, RefreshCw, Search as SearchIcon } from "lucide-react";
import { Fragment, useState } from "react";

import { CandidateList } from "../components/CandidateList";
import { EmptyState } from "../components/EmptyState";
import { IssueDetailPanel } from "../components/IssueDetailPanel";
import { IssueTimeline } from "../components/IssueTimeline";
import { IssueTile } from "../components/IssueTile";
import { SyncingBadge } from "../components/SyncingBadge";
import { Button } from "../components/ui/button";
import { Skeleton } from "../components/ui/skeleton";
import { triggerAutoSearch } from "../gen/omnibus/v1/search-SearchService_connectquery";
import { getSeries, refreshSeries } from "../gen/omnibus/v1/series-SeriesService_connectquery";
import { relativeTime } from "../lib/time";

// SeriesDetail renders the GetSeries header + issue grid. While the series is still
// importing (have < total) it polls GetSeries so issues + covers appear as they land, then
// stops polling once caught up (D-06).
export function SeriesDetail({
  seriesId,
  onBack,
}: {
  seriesId: bigint;
  onBack?: () => void;
}) {
  const [selectedIssueId, setSelectedIssueId] = useState<bigint | null>(null);
  const [searchActive, setSearchActive] = useState(false);

  const detail = useQuery(
    getSeries,
    { seriesId },
    {
      retry: false,
      refetchInterval: (query) => {
        const series = query.state.data?.series;
        if (series && series.haveIssues < series.totalIssues) {
          return 2000; // keep polling while syncing
        }
        return false; // caught up — stop polling
      },
    },
  );

  const refresh = useMutation(refreshSeries, {
    onSuccess: () => void detail.refetch(),
  });

  // The "Auto-grab & download" path: run the inline search-and-auto-grab and report the
  // outcome. On success re-poll GetSeries so the issue's status updates.
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
    return (
      <div className="flex flex-col gap-8">
        <div className="flex gap-6">
          <Skeleton className="aspect-[2/3] w-28 rounded-xl" />
          <div className="flex flex-1 flex-col gap-3 pt-2">
            <Skeleton className="h-8 w-64" />
            <Skeleton className="h-4 w-32" />
            <Skeleton className="h-4 w-40" />
          </div>
        </div>
        <Skeleton className="h-64 w-full rounded-xl" />
      </div>
    );
  }

  const { series, issues, publisher, storyArcs } = detail.data;
  const importing = series.haveIssues < series.totalIssues;
  const refreshing = refresh.isPending || importing;
  const lastRefreshed = relativeTime(series.lastRefreshedAt);
  const pct =
    series.totalIssues > 0
      ? Math.min(100, Math.round((series.haveIssues / series.totalIssues) * 100))
      : 0;

  return (
    <div className="flex flex-col gap-8">
      {onBack ? (
        <Button
          variant="ghost"
          size="sm"
          onClick={onBack}
          className="-ml-2 w-fit gap-1.5 text-muted-foreground"
        >
          <ArrowLeft className="size-4" />
          Library
        </Button>
      ) : null}

      {/* hero header */}
      <header className="flex flex-col gap-5 border-b border-border pb-6 sm:flex-row sm:gap-6">
        {series.coverUrl ? (
          <img
            src={series.coverUrl}
            alt={`Cover for ${series.name}`}
            className="aspect-[2/3] w-28 shrink-0 self-start rounded-xl object-cover shadow-xl shadow-black/40 ring-1 ring-border"
          />
        ) : (
          <div className="flex aspect-[2/3] w-28 shrink-0 items-center justify-center self-start rounded-xl border border-border bg-secondary p-2 text-center font-display text-xs font-semibold text-muted-foreground">
            {series.name}
          </div>
        )}

        <div className="flex flex-1 flex-col gap-3">
          <div className="flex flex-wrap items-center gap-3">
            <h1 className="font-display text-3xl font-semibold tracking-tight text-foreground">
              {series.name}
            </h1>
            {refreshing ? <SyncingBadge /> : null}
          </div>

          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-muted-foreground">
            {publisher ? <span>{publisher}</span> : null}
            {publisher ? <span className="text-border-strong">·</span> : null}
            <span className="font-mono text-tn-fg-dark">
              {series.haveIssues}/{series.totalIssues} issues
            </span>
            {series.startYear ? (
              <>
                <span className="text-border-strong">·</span>
                <span className="font-mono">{series.startYear}</span>
              </>
            ) : null}
          </div>

          {/* completion bar */}
          <div className="h-1.5 w-full max-w-sm overflow-hidden rounded-full bg-secondary">
            <div
              className="h-full rounded-full bg-primary shadow-[0_0_10px_0_rgba(122,162,247,0.7)] transition-all"
              style={{ width: `${pct}%` }}
            />
          </div>

          <div className="flex flex-wrap items-center gap-3 pt-1">
            <Button
              variant="secondary"
              size="sm"
              disabled={refreshing}
              onClick={() => refresh.mutate({ seriesId: series.id })}
              className="gap-1.5"
            >
              <RefreshCw className={`size-3.5 ${refreshing ? "animate-spin" : ""}`} />
              Refresh now
            </Button>
            {importing ? (
              <span className="text-xs text-muted-foreground">
                Syncing issues from ComicVine… this updates automatically.
              </span>
            ) : refresh.isPending ? (
              <span className="text-xs text-muted-foreground">
                Refreshing metadata from ComicVine… this updates automatically.
              </span>
            ) : lastRefreshed ? (
              <span className="font-mono text-xs text-muted-foreground">
                Last refreshed {lastRefreshed}
              </span>
            ) : null}
          </div>

          {refresh.isError ? (
            <span className="text-xs text-tn-red">
              Couldn't start a refresh. Try again in a moment.
            </span>
          ) : null}

          {storyArcs.length > 0 ? (
            <div className="flex flex-wrap items-center gap-1.5 pt-1">
              <span className="font-mono text-[0.65rem] uppercase tracking-wider text-muted-foreground">
                Arcs
              </span>
              {storyArcs.map((a) => (
                <span
                  key={a.id.toString()}
                  className="rounded-full border border-border bg-secondary/60 px-2 py-0.5 text-xs text-tn-fg-dark"
                >
                  {a.name}
                </span>
              ))}
            </div>
          ) : null}
        </div>
      </header>

      {/* issues */}
      <section className="flex flex-col gap-4">
        <h2 className="font-display text-xl font-semibold tracking-tight">Issues</h2>
        <div className="grid grid-cols-[repeat(auto-fill,minmax(140px,1fr))] gap-4">
          {issues.map((issue) => (
            <Fragment key={issue.id.toString()}>
              <button
                type="button"
                onClick={() => {
                  setSelectedIssueId(selectedIssueId === issue.id ? null : issue.id);
                  setSearchActive(false);
                  autoGrab.reset();
                }}
                aria-pressed={selectedIssueId === issue.id}
                className={`group rounded-lg text-left transition-all focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/60 ${
                  selectedIssueId === issue.id
                    ? "ring-2 ring-primary"
                    : "hover:-translate-y-0.5"
                }`}
              >
                <IssueTile issue={issue} />
              </button>

              {selectedIssueId === issue.id ? (
                <section className="col-span-full flex animate-fade-up flex-col gap-6 rounded-xl border border-border bg-tn-night/40 p-5">
                  <IssueDetailPanel issueId={issue.id} />

                  <section className="flex flex-col gap-4">
                    <div className="flex flex-wrap items-center gap-3">
                      <h2 className="font-display text-lg font-semibold tracking-tight">
                        Candidates
                      </h2>
                      <div className="ml-auto flex items-center gap-2">
                        <Button
                          size="sm"
                          disabled={autoGrab.isPending}
                          onClick={() => autoGrab.mutate({ issueId: issue.id })}
                          className="gap-1.5"
                        >
                          <Download className="size-3.5" />
                          {autoGrab.isPending ? "Auto-grabbing…" : "Auto-grab & download"}
                        </Button>
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() => setSearchActive(true)}
                          className="gap-1.5"
                        >
                          <SearchIcon className="size-3.5" />
                          Search &amp; choose
                        </Button>
                      </div>
                    </div>
                    {autoGrab.isError ? (
                      <p className="text-sm text-tn-red">
                        Couldn't complete the auto-grab. Check the server and download client
                        are reachable, then try again.
                      </p>
                    ) : autoGrab.data ? (
                      autoGrab.data.grabbed ? (
                        <p className="text-sm text-tn-green">
                          Grabbed {autoGrab.data.title}. Handed off to the download client —
                          see the timeline below.
                        </p>
                      ) : (
                        <p className="text-sm text-muted-foreground">
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
                    <h2 className="font-display text-lg font-semibold tracking-tight">Timeline</h2>
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
