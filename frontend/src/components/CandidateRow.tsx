import type { Candidate } from "../gen/omnibus/v1/search_pb";

// formatBytes renders a release size as a short human string. 0 (unknown) renders "—".
function formatBytes(bytes: bigint): string {
  if (bytes <= 0n) return "—";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = Number(bytes);
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`;
}

// CandidateRow renders one ranked release candidate: the title (text-base), score +
// provider + size as text-sm metadata (score is NOT a badge, per UI-SPEC), and a per-row
// "Grab" primary CTA that hands the release off to the download client. While a grab is
// in flight the CTA is disabled.
export function CandidateRow({
  candidate,
  onGrab,
  grabbing,
}: {
  candidate: Candidate;
  onGrab: (candidate: Candidate) => void;
  grabbing: boolean;
}) {
  return (
    <div className="flex items-center justify-between gap-4 rounded border border-slate-200 bg-slate-50 p-4">
      <div className="flex flex-col gap-1">
        <span className="text-base">{candidate.title}</span>
        <span className="text-sm text-slate-600">
          Score {candidate.score.toFixed(0)} · {candidate.provider} · {formatBytes(candidate.sizeBytes)}
        </span>
      </div>
      <button
        type="button"
        disabled={grabbing}
        onClick={() => onGrab(candidate)}
        className="rounded bg-blue-600 px-4 py-2 text-sm font-semibold text-white disabled:cursor-not-allowed disabled:bg-slate-400"
      >
        Grab
      </button>
    </div>
  );
}
