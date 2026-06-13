import { JobRunState } from "../gen/omnibus/v1/jobs_pb";

// stateMeta maps each JobRunState to its label + a dark-tuned tint. Colour-family names are
// load-bearing for tests (RUNNING → /violet/, FAILED → /red/).
const stateMeta: Record<JobRunState, { label: string; className: string }> = {
  [JobRunState.UNSPECIFIED]: {
    label: "Unknown",
    className: "border-slate-500/25 bg-slate-500/12 text-slate-300",
  },
  [JobRunState.QUEUED]: {
    label: "Queued",
    className: "border-slate-500/25 bg-slate-500/12 text-slate-300",
  },
  [JobRunState.RUNNING]: {
    label: "Running",
    className: "border-violet-400/30 bg-violet-500/12 text-violet-300",
  },
  [JobRunState.COMPLETED]: {
    label: "Completed",
    className: "border-green-400/30 bg-green-500/12 text-green-300",
  },
  [JobRunState.FAILED]: {
    label: "Failed",
    className: "border-red-400/30 bg-red-500/12 text-red-300",
  },
  [JobRunState.CANCELLED]: {
    label: "Cancelled",
    className: "border-slate-600/20 bg-slate-600/10 text-slate-500",
  },
};

// JobStateBadge renders one of the five job-run states as a mono pill, falling back to a
// neutral badge for an unknown/unspecified state.
export function JobStateBadge({ state }: { state: JobRunState }) {
  const meta = stateMeta[state] ?? stateMeta[JobRunState.UNSPECIFIED];
  return (
    <span
      className={`inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 font-mono text-[0.65rem] font-medium uppercase tracking-wider ${meta.className}`}
    >
      <span className="size-1.5 rounded-full bg-current" aria-hidden="true" />
      {meta.label}
    </span>
  );
}
