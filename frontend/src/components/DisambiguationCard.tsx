import type { Series } from "../gen/omnibus/v1/series_pb";

// DisambiguationCard shows one search candidate richly enough to distinguish same-title
// volumes (D-09): cover thumbnail, name, start year, publisher, issue count, optional
// description snippet, and an "Add to Library" CTA.
export function DisambiguationCard({
  candidate,
  onAdd,
}: {
  candidate: Series;
  onAdd: (candidate: Series) => void;
}) {
  return (
    <div className="flex flex-col gap-1 rounded bg-slate-100 p-4">
      {candidate.coverUrl ? (
        <img
          src={candidate.coverUrl}
          alt={`Cover for ${candidate.name}`}
          className="aspect-[2/3] w-full rounded object-cover"
        />
      ) : (
        <div className="flex aspect-[2/3] w-full items-center justify-center rounded bg-slate-200 text-sm font-semibold text-slate-500">
          {candidate.name}
        </div>
      )}
      <span className="text-base font-semibold">{candidate.name}</span>
      <span className="text-sm text-slate-600">{candidate.startYear}</span>
      <span className="text-sm text-slate-600">{candidate.publisher}</span>
      <span className="text-sm text-slate-600">{candidate.totalIssues} issues</span>
      <button
        type="button"
        onClick={() => onAdd(candidate)}
        className="mt-2 rounded bg-blue-600 px-4 py-2 text-sm font-semibold text-white"
      >
        Add to Library
      </button>
    </div>
  );
}
