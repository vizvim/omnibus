import { useQuery, useMutation } from "@connectrpc/connect-query";
import { useState } from "react";

import { DisambiguationCard } from "../components/DisambiguationCard";
import { EmptyState } from "../components/EmptyState";
import { SearchBox } from "../components/SearchBox";
import type { Series } from "../gen/omnibus/v1/series_pb";
import { addSeries, searchComicVine } from "../gen/omnibus/v1/series-SeriesService_connectquery";

// Search drives SearchComicVine + AddSeries through the generated connect-query hooks.
export function Search({ onAdded }: { onAdded: (seriesId: bigint) => void }) {
  const [query, setQuery] = useState("");

  const search = useQuery(
    searchComicVine,
    { query },
    { enabled: query !== "", retry: false },
  );

  const add = useMutation(addSeries, {
    onSuccess: (resp) => {
      if (resp.series) {
        onAdded(resp.series.id);
      }
    },
  });

  function handleAdd(candidate: Series) {
    add.mutate({ comicvineVolumeId: candidate.comicvineVolumeId });
  }

  return (
    <div className="flex flex-col gap-6">
      <SearchBox onSearch={setQuery} pending={search.isFetching} />

      {search.isError ? (
        <EmptyState
          heading="Couldn't reach ComicVine"
          body="Couldn't reach ComicVine. Check your connection and API key, then try again."
          cta={
            <button
              type="button"
              onClick={() => search.refetch()}
              className="rounded bg-blue-600 px-4 py-2 text-sm font-semibold text-white"
            >
              Retry
            </button>
          }
        />
      ) : null}

      {search.isSuccess && search.data.candidates.length === 0 ? (
        <EmptyState
          heading="No matches found"
          body={`No ComicVine series matched "${query}". Try a different title or spelling.`}
        />
      ) : null}

      {search.isSuccess && search.data.candidates.length > 0 ? (
        <div className="grid grid-cols-[repeat(auto-fill,minmax(160px,1fr))] gap-4">
          {search.data.candidates.map((candidate) => (
            <DisambiguationCard
              key={candidate.comicvineVolumeId.toString()}
              candidate={candidate}
              onAdd={handleAdd}
            />
          ))}
        </div>
      ) : null}
    </div>
  );
}
