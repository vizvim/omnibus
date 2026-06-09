import { Loader2 } from "lucide-react";

// SyncingBadge shows the violet "syncing…" indicator while a series imports (D-06).
// It carries aria-busy + a spinning glyph; it is informational, not accent.
export function SyncingBadge() {
  return (
    <span
      aria-busy="true"
      className="inline-flex items-center gap-1 rounded bg-violet-100 px-2 py-0.5 text-sm font-semibold text-violet-600"
    >
      <Loader2 className="h-3 w-3 animate-spin" aria-hidden="true" />
      syncing…
    </span>
  );
}
