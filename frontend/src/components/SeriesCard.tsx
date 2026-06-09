import type { Series } from "../gen/omnibus/v1/series_pb";
import { SyncingBadge } from "./SyncingBadge";

// SeriesCard shows one watched series: cover (or placeholder), name, publisher, the
// have/total issue counts, and a syncing badge while importing (have < total — D-06).
export function SeriesCard({ series, onOpen }: { series: Series; onOpen: (id: bigint) => void }) {
  const importing = series.haveIssues < series.totalIssues;
  return (
    <button
      type="button"
      onClick={() => onOpen(series.id)}
      className="flex flex-col gap-1 rounded bg-slate-100 p-4 text-left"
    >
      {series.coverUrl ? (
        <img
          src={series.coverUrl}
          alt={`Cover for ${series.name}`}
          className="aspect-[2/3] w-full rounded object-cover"
        />
      ) : (
        <div className="flex aspect-[2/3] w-full items-center justify-center rounded bg-slate-200 text-sm font-semibold text-slate-500">
          {series.name}
        </div>
      )}
      <span className="text-base font-semibold">{series.name}</span>
      {series.publisher ? <span className="text-sm text-slate-600">{series.publisher}</span> : null}
      <div className="flex items-center justify-between gap-2">
        <span className="text-sm text-slate-600">
          {series.haveIssues}/{series.totalIssues} issues
        </span>
        {importing ? <SyncingBadge /> : null}
      </div>
    </button>
  );
}
