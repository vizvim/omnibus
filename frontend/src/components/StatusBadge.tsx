import { IssueStatus } from "../gen/omnibus/v1/common_pb";

// statusMeta maps each IssueStatus to its label + a dark-tuned tint. The colour family
// names (blue/violet/green/red/slate) are intentional: they encode the ADR 0004 state
// machine and a few tests assert on the family word (e.g. FAILED → /red/).
const statusMeta: Record<IssueStatus, { label: string; className: string }> = {
  [IssueStatus.UNSPECIFIED]: {
    label: "Unknown",
    className: "border-slate-500/25 bg-slate-500/12 text-slate-300",
  },
  [IssueStatus.WANTED]: {
    label: "Wanted",
    className: "border-blue-400/30 bg-blue-500/12 text-blue-300",
  },
  [IssueStatus.SNATCHED]: {
    label: "Snatched",
    className: "border-violet-400/30 bg-violet-500/12 text-violet-300",
  },
  [IssueStatus.DOWNLOADED]: {
    label: "Downloaded",
    className: "border-green-400/30 bg-green-500/12 text-green-300",
  },
  [IssueStatus.ARCHIVED]: {
    label: "Archived",
    className: "border-slate-400/25 bg-slate-500/12 text-slate-300",
  },
  [IssueStatus.SKIPPED]: {
    label: "Skipped",
    className: "border-slate-500/20 bg-slate-500/10 text-slate-400",
  },
  [IssueStatus.FAILED]: {
    label: "Failed",
    className: "border-red-400/30 bg-red-500/12 text-red-300",
  },
  [IssueStatus.IGNORED]: {
    label: "Ignored",
    className: "border-slate-600/20 bg-slate-600/10 text-slate-500",
  },
};

// StatusBadge renders one of the 7 IssueStatus values (SER-02) as a small mono pill with a
// status dot, or a neutral fallback for an unknown/unspecified status.
export function StatusBadge({ status }: { status: IssueStatus }) {
  const meta = statusMeta[status] ?? statusMeta[IssueStatus.UNSPECIFIED];
  return (
    <span
      className={`inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 font-mono text-[0.65rem] font-medium uppercase tracking-wider ${meta.className}`}
    >
      <span className="size-1.5 rounded-full bg-current" aria-hidden="true" />
      {meta.label}
    </span>
  );
}
