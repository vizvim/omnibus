import { useState } from "react";

import { Activity } from "./pages/Activity";
import { Library } from "./pages/Library";
import { Search } from "./pages/Search";
import { SeriesDetail } from "./pages/SeriesDetail";

type Route = "library" | "search" | "detail" | "activity";

// App is the minimal-functional shell: a header with Library/Search nav and a small
// internal router (no router library needed for the Phase 2 surface). initialRoute /
// initialSeriesId let tests mount a specific screen directly.
export function App({
  initialRoute = "library",
  initialSeriesId,
}: {
  initialRoute?: Route;
  initialSeriesId?: bigint;
}) {
  const [route, setRoute] = useState<Route>(initialRoute);
  const [seriesId, setSeriesId] = useState<bigint | undefined>(initialSeriesId);

  function openSeries(id: bigint) {
    setSeriesId(id);
    setRoute("detail");
  }

  return (
    <div className="mx-auto flex max-w-5xl flex-col gap-6 p-8">
      <header className="flex items-center justify-between border-b border-slate-200 pb-4">
        <h1 className="text-2xl font-semibold">omnibus</h1>
        <nav className="flex gap-4 text-sm font-semibold">
          <button type="button" onClick={() => setRoute("library")} className="text-blue-600">
            Library
          </button>
          <button type="button" onClick={() => setRoute("search")} className="text-blue-600">
            Search
          </button>
          <button type="button" onClick={() => setRoute("activity")} className="text-blue-600">
            Activity
          </button>
        </nav>
      </header>

      {route === "library" ? (
        <Library onOpenSeries={openSeries} onSearch={() => setRoute("search")} />
      ) : null}
      {route === "search" ? <Search onAdded={openSeries} /> : null}
      {route === "activity" ? <Activity /> : null}
      {route === "detail" && seriesId !== undefined ? <SeriesDetail seriesId={seriesId} /> : null}
    </div>
  );
}
