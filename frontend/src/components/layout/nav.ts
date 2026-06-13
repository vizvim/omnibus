import { BookMarked, Compass, Radio, Settings as SettingsIcon, Waves } from "lucide-react";
import type { LucideIcon } from "lucide-react";

// Route is the full set of internal screens. "detail" is reachable from Library/Search
// but is not a top-level nav destination.
export type Route = "library" | "search" | "detail" | "activity" | "indexers" | "settings";

// NavTarget describes where a navigation goes plus optional payloads (the opened series,
// or a seed query handed to the Discover screen from the command palette).
export interface NavTarget {
  seriesId?: bigint;
  query?: string;
}

export type Navigate = (route: Route, target?: NavTarget) => void;

export interface NavItem {
  route: Exclude<Route, "detail">;
  label: string;
  hint: string;
  icon: LucideIcon;
}

// NAV_ITEMS is the single source for both the sidebar rail and the command palette, so the
// two never drift. Labels lean into the product verbs: a comic library you discover into.
export const NAV_ITEMS: NavItem[] = [
  { route: "library", label: "Library", hint: "Your watched series", icon: BookMarked },
  { route: "search", label: "Discover", hint: "Find series on ComicVine", icon: Compass },
  { route: "activity", label: "Activity", hint: "Background job runs", icon: Waves },
  { route: "indexers", label: "Indexers", hint: "Newznab sources", icon: Radio },
  { route: "settings", label: "Settings", hint: "Clients & renaming", icon: SettingsIcon },
];

// titleFor returns the human page title for a route, used by the topbar + document title.
export function titleFor(route: Route): string {
  switch (route) {
    case "library":
      return "Library";
    case "search":
      return "Discover";
    case "detail":
      return "Series";
    case "activity":
      return "Activity";
    case "indexers":
      return "Indexers";
    case "settings":
      return "Settings";
  }
}
