import { useMutation, useQuery } from "@connectrpc/connect-query";
import { Compass, SearchX, WifiOff } from "lucide-react";
import { useEffect, useState } from "react";

import { DisambiguationCard } from "../components/DisambiguationCard";
import { EmptyState } from "../components/EmptyState";
import { PageHeader } from "../components/layout/PageHeader";
import { SearchBox } from "../components/SearchBox";
import { Button } from "../components/ui/button";
import type { Series } from "../gen/omnibus/v1/series_pb";
import { addSeries, searchComicVine } from "../gen/omnibus/v1/series-SeriesService_connectquery";

// Search drives SearchComicVine + AddSeries through the generated connect-query hooks. A
// seed (e.g. from the ⌘K palette) pre-fills and runs the search on mount.
export function Search({ seed = "", onAdded }: { seed?: string; onAdded: (seriesId: bigint) => void }) {
  const [query, setQuery] = useState(seed);

  // When the palette hands over a fresh seed, adopt it as the active query.
  useEffect(() => {
    if (seed) setQuery(seed);
  }, [seed]);

  const search = useQuery(searchComicVine, { query }, { enabled: query !== "", retry: false });

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
    <div className="flex flex-col gap-8">
      <PageHeader
        eyebrow="ComicVine"
        title="Discover"
        description="Search ComicVine and add a series to start tracking its issues."
      />

      <SearchBox key={seed} initialValue={seed} onSearch={setQuery} pending={search.isFetching} />

      {query === "" ? (
        <EmptyState
          icon={<Compass />}
          heading="Find something to read"
          body="Type a series title above — Saga, Daredevil, Invincible — and omnibus will look it up on ComicVine."
        />
      ) : null}

      {search.isError ? (
        <EmptyState
          icon={<WifiOff />}
          heading="Couldn't reach ComicVine"
          body="Couldn't reach ComicVine. Check your connection and API key, then try again."
          cta={
            <Button variant="secondary" onClick={() => search.refetch()}>
              Retry
            </Button>
          }
        />
      ) : null}

      {search.isSuccess && search.data.candidates.length === 0 ? (
        <EmptyState
          icon={<SearchX />}
          heading="No matches found"
          body={`No ComicVine series matched "${query}". Try a different title or spelling.`}
        />
      ) : null}

      {search.isSuccess && search.data.candidates.length > 0 ? (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-[repeat(auto-fill,minmax(168px,1fr))]">
          {search.data.candidates.map((candidate, i) => (
            <div
              key={candidate.comicvineVolumeId.toString()}
              className="animate-fade-up"
              style={{ animationDelay: `${Math.min(i, 12) * 35}ms` }}
            >
              <DisambiguationCard candidate={candidate} onAdd={handleAdd} />
            </div>
          ))}
        </div>
      ) : null}
    </div>
  );
}
