import { JobRunState } from "../gen/omnibus/v1/jobs_pb";

// stateMeta maps each JobRunState to its UI-SPEC label + tint classes. These are
// job-run states (NOT IssueStatus): muted tints so they read as metadata, not chrome.
const stateMeta: Record<JobRunState, { label: string; className: string }> = {
  [JobRunState.UNSPECIFIED]: { label: "Unknown", className: "text-slate-500 bg-slate-100" },
  [JobRunState.QUEUED]: { label: "Queued", className: "text-slate-500 bg-slate-100" },
  [JobRunState.RUNNING]: { label: "Running", className: "text-violet-600 bg-violet-100" },
  [JobRunState.COMPLETED]: { label: "Completed", className: "text-green-700 bg-green-100" },
  [JobRunState.FAILED]: { label: "Failed", className: "text-red-600 bg-red-100" },
  [JobRunState.CANCELLED]: { label: "Cancelled", className: "text-slate-400 bg-slate-50" },
};

// JobStateBadge renders one of the five job-run states with its UI-SPEC tint, falling
// back to a neutral badge for an unknown/unspecified state.
export function JobStateBadge({ state }: { state: JobRunState }) {
  const meta = stateMeta[state] ?? stateMeta[JobRunState.UNSPECIFIED];
  return (
    <span className={`inline-flex items-center rounded px-2 py-0.5 text-sm font-semibold ${meta.className}`}>
      {meta.label}
    </span>
  );
}
