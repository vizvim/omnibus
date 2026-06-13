import { Download, HardDrive } from "lucide-react";

import type { Candidate } from "../gen/omnibus/v1/search_pb";
import { Button } from "./ui/button";

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

// scoreTone tints the score chip by confidence so a strong match reads at a glance.
function scoreTone(score: number): string {
  if (score >= 75) return "border-tn-green/30 bg-tn-green/12 text-tn-green";
  if (score >= 50) return "border-tn-yellow/30 bg-tn-yellow/12 text-tn-yellow";
  return "border-tn-red/30 bg-tn-red/12 text-tn-red";
}

// CandidateRow renders one ranked release candidate: the title, the score as a tinted mono
// chip, provider + size as muted metadata, and a per-row "Grab" CTA. The literal text
// "Score N" is preserved (a test asserts on it).
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
    <div className="group flex items-center justify-between gap-4 rounded-lg border border-border bg-card/60 p-3.5 transition-colors hover:border-primary/40 hover:bg-card">
      <div className="flex min-w-0 flex-col gap-1.5">
        <span className="truncate text-sm font-medium text-foreground">{candidate.title}</span>
        <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
          <span
            className={`inline-flex items-center rounded border px-1.5 py-0.5 font-mono text-[0.65rem] font-medium ${scoreTone(
              candidate.score,
            )}`}
          >
            Score {candidate.score.toFixed(0)}
          </span>
          <span className="font-mono uppercase tracking-wide">{candidate.provider}</span>
          <span className="inline-flex items-center gap-1 font-mono">
            <HardDrive className="size-3" />
            {formatBytes(candidate.sizeBytes)}
          </span>
        </div>
      </div>
      <Button
        size="sm"
        disabled={grabbing}
        onClick={() => onGrab(candidate)}
        className="shrink-0 gap-1.5"
      >
        <Download className="size-3.5" />
        Grab
      </Button>
    </div>
  );
}
