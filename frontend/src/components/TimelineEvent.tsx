import { IssueEventType } from "../gen/omnibus/v1/common_pb";
import type { IssueEvent } from "../gen/omnibus/v1/search_pb";
import { relativeTime } from "../lib/time";

// eventMeta maps each timeline event type to its label + tint. Colour-family words
// (slate/green/violet/red) are load-bearing for tests: searched = slate, candidate-
// selected = green, snatched = violet (mirrors SNATCHED), failed = red.
const eventMeta: Record<IssueEventType, { label: string; className: string }> = {
  [IssueEventType.UNSPECIFIED]: {
    label: "Event",
    className: "border-slate-500/25 bg-slate-500/12 text-slate-300",
  },
  [IssueEventType.SEARCHED]: {
    label: "Searched",
    className: "border-slate-500/25 bg-slate-500/12 text-slate-300",
  },
  [IssueEventType.CANDIDATE_SELECTED]: {
    label: "Candidate selected",
    className: "border-green-400/30 bg-green-500/12 text-green-300",
  },
  [IssueEventType.SNATCHED]: {
    label: "Snatched",
    className: "border-violet-400/30 bg-violet-500/12 text-violet-300",
  },
  [IssueEventType.FAILED]: {
    label: "Failed",
    className: "border-red-400/30 bg-red-500/12 text-red-300",
  },
  [IssueEventType.DOWNLOADED]: {
    label: "Downloaded",
    className: "border-green-400/30 bg-green-500/12 text-green-300",
  },
  [IssueEventType.PROCESSED]: {
    label: "Processed",
    className: "border-slate-400/25 bg-slate-500/12 text-slate-300",
  },
};

// description renders a short human description: the type label, optionally followed by the
// raw payload detail when present.
function description(event: IssueEvent, label: string): string {
  if (event.detail) {
    return `${label} — ${event.detail}`;
  }
  return label;
}

// TimelineEvent renders one per-issue event row: a tinted type badge, a description, and a
// relative timestamp. A connector dot + line gives the column a true "timeline" read.
// NOTE: the badge keeps the literal `rounded px-2` classes a test selects on.
export function TimelineEvent({ event }: { event: IssueEvent }) {
  const meta = eventMeta[event.type] ?? eventMeta[IssueEventType.UNSPECIFIED];
  return (
    <div className="flex items-center gap-3 rounded-lg border border-border bg-card/60 px-4 py-3 transition-colors hover:border-border-strong/70">
      <span
        className={`inline-flex shrink-0 items-center rounded px-2 py-0.5 font-mono text-[0.65rem] font-medium uppercase tracking-wider ${meta.className}`}
      >
        {meta.label}
      </span>
      <span className="flex-1 truncate text-sm text-foreground/90">
        {description(event, meta.label)}
      </span>
      <span className="shrink-0 font-mono text-xs text-muted-foreground">
        {relativeTime(event.occurredAt)}
      </span>
    </div>
  );
}
