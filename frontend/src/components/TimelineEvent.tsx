import { IssueEventType } from "../gen/omnibus/v1/common_pb";
import type { IssueEvent } from "../gen/omnibus/v1/search_pb";
import { relativeTime } from "../lib/time";

// eventMeta maps each timeline event type to its label + tint classes. The tints reuse
// the EXISTING badge vocabulary (UI-SPEC) — no new hues: searched = neutral slate,
// candidate-selected = green, snatched = violet (mirrors the SNATCHED status), failed =
// red.
const eventMeta: Record<IssueEventType, { label: string; className: string }> = {
  [IssueEventType.UNSPECIFIED]: { label: "Event", className: "text-slate-500 bg-slate-100" },
  [IssueEventType.SEARCHED]: { label: "Searched", className: "text-slate-500 bg-slate-100" },
  [IssueEventType.CANDIDATE_SELECTED]: { label: "Candidate selected", className: "text-green-700 bg-green-100" },
  [IssueEventType.SNATCHED]: { label: "Snatched", className: "text-violet-600 bg-violet-100" },
  [IssueEventType.FAILED]: { label: "Failed", className: "text-red-600 bg-red-100" },
  // Downloaded/Processed events land in Phase 5; mapped onto the existing tint
  // vocabulary now so an early event renders without a new hue.
  [IssueEventType.DOWNLOADED]: { label: "Downloaded", className: "text-green-700 bg-green-100" },
  [IssueEventType.PROCESSED]: { label: "Processed", className: "text-slate-600 bg-slate-200" },
};

// description renders a short human description for an event: the type label, optionally
// followed by the raw payload detail when present.
function description(event: IssueEvent, label: string): string {
  if (event.detail) {
    return `${label} — ${event.detail}`;
  }
  return label;
}

// TimelineEvent renders one per-issue event row: a type badge (existing tint), a
// text-base description, and a text-sm relative timestamp (OBS-01).
export function TimelineEvent({ event }: { event: IssueEvent }) {
  const meta = eventMeta[event.type] ?? eventMeta[IssueEventType.UNSPECIFIED];
  return (
    <div className="flex items-center gap-2 rounded border border-slate-200 bg-slate-50 p-4">
      <span
        className={`inline-flex items-center rounded px-2 py-0.5 text-sm font-semibold ${meta.className}`}
      >
        {meta.label}
      </span>
      <span className="flex-1 text-base">{description(event, meta.label)}</span>
      <span className="text-sm text-slate-600">{relativeTime(event.occurredAt)}</span>
    </div>
  );
}
