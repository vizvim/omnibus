import { Plus } from "lucide-react";

import type { Series } from "../gen/omnibus/v1/series_pb";
import { Button } from "./ui/button";

// DisambiguationCard shows one ComicVine search candidate richly enough to distinguish
// same-title volumes (D-09): cover, name, start year, publisher, issue count, and an
// "Add to Library" CTA. Year and publisher stay in separate elements so they read as
// distinct facets (and stay independently selectable).
export function DisambiguationCard({
  candidate,
  onAdd,
}: {
  candidate: Series;
  onAdd: (candidate: Series) => void;
}) {
  return (
    <div className="group flex flex-col overflow-hidden rounded-xl border border-border bg-card/70 transition-all duration-200 hover:-translate-y-1 hover:border-primary/40 hover:shadow-xl hover:shadow-black/40">
      <div className="relative aspect-[2/3] w-full overflow-hidden bg-secondary">
        {candidate.coverUrl ? (
          <img
            src={candidate.coverUrl}
            alt={`Cover for ${candidate.name}`}
            className="size-full object-cover transition-transform duration-300 group-hover:scale-[1.04]"
          />
        ) : (
          <div className="flex size-full items-center justify-center p-4 text-center font-display text-sm font-semibold text-muted-foreground">
            {candidate.name}
          </div>
        )}
        <div className="absolute inset-x-0 bottom-0 h-16 bg-gradient-to-t from-card to-transparent" />
        <span className="absolute right-2 top-2 rounded-full border border-border-strong/60 bg-tn-night/80 px-2 py-0.5 font-mono text-[0.65rem] text-tn-fg-dark backdrop-blur">
          {candidate.totalIssues} issues
        </span>
      </div>

      <div className="flex flex-1 flex-col gap-1 p-3.5">
        <span className="line-clamp-2 font-display text-sm font-semibold leading-snug text-foreground">
          {candidate.name}
        </span>
        <div className="flex items-center gap-1.5 font-mono text-xs text-muted-foreground">
          <span>{candidate.startYear}</span>
          <span aria-hidden="true" className="text-border-strong">
            ·
          </span>
          <span className="truncate">{candidate.publisher}</span>
        </div>
        <Button
          variant="secondary"
          size="sm"
          onClick={() => onAdd(candidate)}
          className="mt-2.5 w-full gap-1.5"
        >
          <Plus className="size-3.5" />
          Add to Library
        </Button>
      </div>
    </div>
  );
}
