import { Loader2 } from "lucide-react";

import { useConnectionState } from "../../lib/transport";
import type { ConnectionState as ConnectionStateValue } from "../../lib/useEventStream";

// ConnectionStateIndicator is the presentational topbar pill bound to the unified live-status
// stream (UI-05, D-08/D-09). It reflects three states with the locked tints + mono labels
// (UI-SPEC): live = green dot+glow + "live"; reconnecting = magenta Loader2 spinner +
// aria-busy + "reconnecting…"; offline = red dot + "offline". Color is never the only signal
// (dot/icon + text label), and the whole thing is wrapped in an aria-live="polite" region so
// transitions are announced without over-announcing high-frequency progress. It takes the
// state as a prop so it is unit-testable across all three states.
export function ConnectionStateIndicator({ state }: { state: ConnectionStateValue }) {
  return (
    <div
      aria-live="polite"
      className="mr-1 hidden items-center gap-1.5 rounded-full px-2 py-0.5 font-mono text-[0.65rem] sm:flex"
    >
      {state === "live" ? <LivePill /> : null}
      {state === "reconnecting" ? <ReconnectingPill /> : null}
      {state === "offline" ? <OfflinePill /> : null}
    </div>
  );
}

// LivePill: green dot with the existing topbar glow + mono "live".
function LivePill() {
  return (
    <span className="flex items-center gap-1.5 text-muted-foreground">
      <span
        aria-hidden="true"
        className="size-2 rounded-full bg-tn-green shadow-[0_0_8px_0_rgba(158,206,106,0.8)]"
      />
      live
    </span>
  );
}

// ReconnectingPill: magenta spinner + aria-busy + mono "reconnecting…", plus a visually
// hidden aria-live announcement (announced via the wrapping polite region).
function ReconnectingPill() {
  return (
    <span aria-busy="true" className="flex items-center gap-1.5 text-tn-magenta">
      <Loader2 className="size-3 animate-spin" aria-hidden="true" />
      reconnecting…
      <span className="sr-only">Reconnecting to live updates</span>
    </span>
  );
}

// OfflinePill: red static dot + mono "offline", plus a visually hidden announcement clarifying
// that on-screen data is the last-known status (graceful degradation, not blanked).
function OfflinePill() {
  return (
    <span className="flex items-center gap-1.5 text-tn-red">
      <span aria-hidden="true" className="size-2 rounded-full bg-tn-red" />
      offline
      <span className="sr-only">
        Live updates disconnected. Showing last-known status.
      </span>
    </span>
  );
}

// ConnectionState binds the indicator to the single root stream subscription's state from
// context (D-08), replacing the hardcoded "online" dot in the topbar.
export function ConnectionState() {
  const state = useConnectionState();
  return <ConnectionStateIndicator state={state} />;
}
