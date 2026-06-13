import { useCallback, useEffect, useState } from "react";

import { AppShell } from "./components/layout/AppShell";
import { CommandPalette } from "./components/layout/CommandPalette";
import { titleFor, type Navigate, type Route } from "./components/layout/nav";
import { Activity } from "./pages/Activity";
import { Indexers } from "./pages/Indexers";
import { Library } from "./pages/Library";
import { Search } from "./pages/Search";
import { SeriesDetail } from "./pages/SeriesDetail";
import { Settings } from "./pages/Settings";

// App is the shell + internal router (no router library for this surface). State holds the
// active route, the opened series, a seed query handed to Discover from the command
// palette, and whether ⌘K is open. initialRoute / initialSeriesId let tests mount a
// specific screen directly.
export function App({
  initialRoute = "library",
  initialSeriesId,
}: {
  initialRoute?: Route;
  initialSeriesId?: bigint;
}) {
  const [route, setRoute] = useState<Route>(initialRoute);
  const [seriesId, setSeriesId] = useState<bigint | undefined>(initialSeriesId);
  const [seed, setSeed] = useState("");
  const [commandOpen, setCommandOpen] = useState(false);

  const navigate = useCallback<Navigate>((next, target) => {
    if (target?.seriesId !== undefined) setSeriesId(target.seriesId);
    setSeed(target?.query ?? "");
    setRoute(next);
  }, []);

  const openSeries = useCallback(
    (id: bigint) => navigate("detail", { seriesId: id }),
    [navigate],
  );

  // ⌘K / Ctrl-K toggles the command palette anywhere in the app.
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setCommandOpen((o) => !o);
      }
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  // Keep the document title in sync with the active section.
  useEffect(() => {
    document.title = `${titleFor(route)} · omnibus`;
  }, [route]);

  return (
    <AppShell active={route} onNavigate={navigate} onOpenCommand={() => setCommandOpen(true)}>
      <div key={`${route}:${seriesId ?? ""}`} className="animate-fade-up">
        {route === "library" ? (
          <Library onOpenSeries={openSeries} onSearch={() => navigate("search")} />
        ) : null}
        {route === "search" ? <Search seed={seed} onAdded={openSeries} /> : null}
        {route === "activity" ? <Activity /> : null}
        {route === "indexers" ? <Indexers /> : null}
        {route === "settings" ? <Settings /> : null}
        {route === "detail" && seriesId !== undefined ? (
          <SeriesDetail seriesId={seriesId} onBack={() => navigate("library")} />
        ) : null}
      </div>

      <CommandPalette open={commandOpen} onOpenChange={setCommandOpen} onNavigate={navigate} />
    </AppShell>
  );
}
