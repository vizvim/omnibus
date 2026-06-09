import { IssueStatus } from "../gen/omnibus/v1/common_pb";

// statusMeta maps each IssueStatus to its UI-SPEC label + tint classes (foreground +
// background). The tints are functional encodings of the ADR 0004 state machine, kept
// muted so they read as metadata, not chrome.
const statusMeta: Record<IssueStatus, { label: string; className: string }> = {
  [IssueStatus.UNSPECIFIED]: { label: "Unknown", className: "text-slate-500 bg-slate-100" },
  [IssueStatus.WANTED]: { label: "Wanted", className: "text-blue-700 bg-blue-100" },
  [IssueStatus.SNATCHED]: { label: "Snatched", className: "text-violet-600 bg-violet-100" },
  [IssueStatus.DOWNLOADED]: { label: "Downloaded", className: "text-green-700 bg-green-100" },
  [IssueStatus.ARCHIVED]: { label: "Archived", className: "text-slate-600 bg-slate-200" },
  [IssueStatus.SKIPPED]: { label: "Skipped", className: "text-slate-500 bg-slate-100" },
  [IssueStatus.FAILED]: { label: "Failed", className: "text-red-600 bg-red-100" },
  [IssueStatus.IGNORED]: { label: "Ignored", className: "text-slate-400 bg-slate-50" },
};

// StatusBadge renders one of the 7 IssueStatus values (SER-02) with its tint, or a
// neutral fallback for an unknown/unspecified status.
export function StatusBadge({ status }: { status: IssueStatus }) {
  const meta = statusMeta[status] ?? statusMeta[IssueStatus.UNSPECIFIED];
  return (
    <span className={`inline-flex items-center rounded px-2 py-0.5 text-sm font-semibold ${meta.className}`}>
      {meta.label}
    </span>
  );
}
