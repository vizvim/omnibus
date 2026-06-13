import { useQuery } from "@connectrpc/connect-query";
import { Compass, Library as LibraryIcon, Plus } from "lucide-react";

import { EmptyState } from "../components/EmptyState";
import { PageHeader } from "../components/layout/PageHeader";
import { SeriesCard } from "../components/SeriesCard";
import { Button } from "../components/ui/button";
import { Skeleton } from "../components/ui/skeleton";
import { listSeries } from "../gen/omnibus/v1/series-SeriesService_connectquery";

// gridClass is shared by the loading skeletons and the real card grid so they line up.
// Single column at the base (mobile) breakpoint; tile into an auto-fill cover grid at sm+
// (UI-02 / D-13 single-column reflow).
const gridClass =
  "grid grid-cols-1 gap-4 sm:grid-cols-[repeat(auto-fill,minmax(168px,1fr))]";

// Library lists watched series via the generated ListSeries query: a loading grid, then an
// empty state when there are none, otherwise a grid of cover cards.
export function Library({
  onOpenSeries,
  onSearch,
}: {
  onOpenSeries: (id: bigint) => void;
  onSearch: () => void;
}) {
  const list = useQuery(listSeries, { page: 0 }, { retry: false });

  if (list.isSuccess && list.data.series.length === 0) {
    return (
      <div className="flex flex-col gap-8">
        <PageHeader eyebrow="Your collection" title="Library" />
        <EmptyState
          icon={<Compass />}
          heading="No series in your library yet"
          body="Search ComicVine for a series and add it to start tracking its issues."
          cta={
            <Button onClick={onSearch} className="gap-1.5">
              <Compass className="size-4" />
              Search ComicVine
            </Button>
          }
        />
      </div>
    );
  }

  const series = list.data?.series ?? [];

  return (
    <div className="flex flex-col gap-8">
      <PageHeader
        eyebrow="Your collection"
        title="Library"
        description={
          series.length > 0
            ? `${series.length} series watched`
            : "Loading your watched series…"
        }
        actions={
          <Button onClick={onSearch} className="gap-1.5">
            <Plus className="size-4" />
            Add series
          </Button>
        }
      />

      {list.isLoading ? (
        <div className={gridClass}>
          {Array.from({ length: 10 }).map((_, i) => (
            <div key={i} className="flex flex-col gap-2">
              <Skeleton className="aspect-[2/3] w-full rounded-xl" />
              <Skeleton className="h-4 w-3/4" />
              <Skeleton className="h-3 w-1/2" />
            </div>
          ))}
        </div>
      ) : (
        <div className={gridClass}>
          {series.map((s, i) => (
            <div
              key={s.id.toString()}
              className="animate-fade-up"
              style={{ animationDelay: `${Math.min(i, 12) * 35}ms` }}
            >
              <SeriesCard series={s} onOpen={onOpenSeries} />
            </div>
          ))}
        </div>
      )}

      {!list.isLoading && series.length === 0 ? (
        <EmptyState
          icon={<LibraryIcon />}
          heading="Nothing to show"
          body="Your library query returned no series."
        />
      ) : null}
    </div>
  );
}
