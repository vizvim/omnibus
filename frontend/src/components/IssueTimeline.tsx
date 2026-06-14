import { useQuery } from "@connectrpc/connect-query";
import { WifiOff } from "lucide-react";

import { getIssueTimeline } from "../gen/omnibus/v1/search-SearchService_connectquery";
import { useConnectionState } from "../lib/transport";
import { EmptyState } from "./EmptyState";
import { TimelineEvent } from "./TimelineEvent";

// IssueTimeline renders the per-issue event timeline (OBS-01): a single-column
// chronological list, most-recent at the top (the focal point). The live stream now drives
// updates — stream timeline/download-progress events invalidate this query so new events
// appear without polling (D-08/D-10). Polling is demoted to an offline-only fallback: it runs
// only while the stream is disconnected, so the network tab shows no periodic GET while live.
export function IssueTimeline({ issueId }: { issueId: bigint }) {
  const connection = useConnectionState();
  const timeline = useQuery(
    getIssueTimeline,
    { issueId },
    {
      retry: false,
      // Fallback poll only when the live stream is offline (D-10); the stream is the default
      // path otherwise.
      refetchInterval: connection === "offline" ? 5000 : false,
    },
  );

  if (timeline.isError) {
    return (
      <EmptyState
        icon={<WifiOff />}
        heading="Couldn't reach the server"
        body="Couldn't reach the server. Check your connection, then try again."
      />
    );
  }

  const events = timeline.data?.events ?? [];

  if (timeline.isSuccess && events.length === 0) {
    return (
      <EmptyState
        heading="Nothing here yet"
        body="Search events appear here once omnibus looks for this issue. Trigger a search to get started."
      />
    );
  }

  // Most-recent first (the focal point). The service returns events in occurred_at
  // ascending order, so reverse a shallow copy for display.
  const ordered = [...events].reverse();

  return (
    <div className="flex flex-col gap-2">
      {ordered.map((event) => (
        <TimelineEvent key={event.id.toString()} event={event} />
      ))}
    </div>
  );
}
