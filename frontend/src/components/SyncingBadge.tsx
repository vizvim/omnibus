import { Loader2 } from "lucide-react";

// SyncingBadge shows the "syncing…" indicator while a series imports (D-06). It carries
// aria-busy + a spinning glyph and uses the magenta accent — informational, not chrome.
export function SyncingBadge() {
  return (
    <span
      aria-busy="true"
      className="inline-flex items-center gap-1.5 rounded-full border border-tn-magenta/30 bg-tn-magenta/12 px-2 py-0.5 font-mono text-[0.65rem] font-medium uppercase tracking-wider text-tn-magenta"
    >
      <Loader2 className="size-3 animate-spin" aria-hidden="true" />
      syncing…
    </span>
  );
}
