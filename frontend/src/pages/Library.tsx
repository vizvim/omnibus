import { useQuery } from "@connectrpc/connect-query";

import { EmptyState } from "../components/EmptyState";
import { SeriesCard } from "../components/SeriesCard";
import { listSeries } from "../gen/omnibus/v1/series-SeriesService_connectquery";

// Library lists watched series via the generated ListSeries query: an empty state when
// there are none, otherwise a grid of series cards.
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
      <EmptyState
        heading="No series in your library yet"
        body="Search ComicVine for a series and add it to start tracking its issues."
        cta={
          <button
            type="button"
            onClick={onSearch}
            className="rounded bg-blue-600 px-4 py-2 text-sm font-semibold text-white"
          >
            Search ComicVine
          </button>
        }
      />
    );
  }

  return (
    <div className="flex flex-col gap-6">
      <div className="flex justify-end">
        <button
          type="button"
          onClick={onSearch}
          className="rounded bg-blue-600 px-4 py-2 text-sm font-semibold text-white"
        >
          Add Series
        </button>
      </div>
      <div className="grid grid-cols-[repeat(auto-fill,minmax(160px,1fr))] gap-4">
        {list.data?.series.map((series) => (
          <SeriesCard key={series.id.toString()} series={series} onOpen={onOpenSeries} />
        ))}
      </div>
    </div>
  );
}
