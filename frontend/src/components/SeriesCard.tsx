import type { Series } from "../gen/omnibus/v1/series_pb";
import { SyncingBadge } from "./SyncingBadge";

// SeriesCard shows one watched series as a cover-forward tile: cover (or a typographic
// placeholder), a completion bar, name, publisher, the have/total issue counts, and a
// syncing badge while importing (have < total — D-06).
export function SeriesCard({ series, onOpen }: { series: Series; onOpen: (id: bigint) => void }) {
  const importing = series.haveIssues < series.totalIssues;
  const pct =
    series.totalIssues > 0
      ? Math.min(100, Math.round((series.haveIssues / series.totalIssues) * 100))
      : 0;

  return (
    <button
      type="button"
      onClick={() => onOpen(series.id)}
      className="group flex flex-col overflow-hidden rounded-xl border border-border bg-card/70 text-left transition-all duration-200 hover:-translate-y-1 hover:border-primary/40 hover:shadow-xl hover:shadow-black/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/60"
    >
      <div className="relative aspect-[2/3] w-full overflow-hidden bg-secondary">
        {series.coverUrl ? (
          <img
            src={series.coverUrl}
            alt={`Cover for ${series.name}`}
            className="size-full object-cover transition-transform duration-300 group-hover:scale-[1.04]"
          />
        ) : (
          <div className="flex size-full items-center justify-center p-4 text-center font-display text-base font-semibold text-muted-foreground">
            {series.name}
          </div>
        )}
        <div className="absolute inset-x-0 bottom-0 h-20 bg-gradient-to-t from-card via-card/40 to-transparent" />
        {importing ? (
          <div className="absolute left-2 top-2">
            <SyncingBadge />
          </div>
        ) : null}
        {/* completion bar pinned to the cover base */}
        <div className="absolute inset-x-0 bottom-0 h-1 bg-tn-night/60">
          <div
            className="h-full bg-primary shadow-[0_0_10px_0_rgba(122,162,247,0.8)] transition-all"
            style={{ width: `${pct}%` }}
          />
        </div>
      </div>

      <div className="flex flex-col gap-1 p-3.5">
        <span className="line-clamp-1 font-display text-sm font-semibold text-foreground">
          {series.name}
        </span>
        {series.publisher ? (
          <span className="truncate text-xs text-muted-foreground">{series.publisher}</span>
        ) : null}
        <span className="mt-0.5 font-mono text-xs text-tn-fg-dark/80">
          {series.haveIssues}/{series.totalIssues} issues
        </span>
      </div>
    </button>
  );
}
