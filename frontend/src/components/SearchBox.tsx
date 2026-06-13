import { Search } from "lucide-react";
import { useState } from "react";

import { Button } from "./ui/button";

// SearchBox is the ComicVine query input + CTA. It owns the input value (seedable via
// initialValue, e.g. from the ⌘K palette) and calls onSearch with the trimmed query.
export function SearchBox({
  onSearch,
  pending,
  initialValue = "",
}: {
  onSearch: (query: string) => void;
  pending: boolean;
  initialValue?: string;
}) {
  const [value, setValue] = useState(initialValue);

  return (
    <form
      className="flex gap-2"
      onSubmit={(e) => {
        e.preventDefault();
        const q = value.trim();
        if (q) {
          onSearch(q);
        }
      }}
    >
      <div className="relative flex-1">
        <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
        <input
          type="text"
          value={value}
          onChange={(e) => setValue(e.target.value)}
          placeholder="Search ComicVine for a series…"
          autoFocus
          className="h-11 w-full rounded-lg border border-input bg-tn-night/50 pl-10 pr-3 text-base text-foreground shadow-inner transition-colors placeholder:text-muted-foreground/70 focus-visible:border-primary/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40"
        />
      </div>
      <Button type="submit" size="lg" disabled={pending}>
        Search ComicVine
      </Button>
    </form>
  );
}
