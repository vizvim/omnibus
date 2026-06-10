import { useQuery } from "@connectrpc/connect-query";

import { getIssueTimeline } from "../gen/omnibus/v1/search-SearchService_connectquery";
import { EmptyState } from "./EmptyState";
import { TimelineEvent } from "./TimelineEvent";

// IssueTimeline renders the per-issue event timeline (OBS-01): a single-column
// chronological list, most-recent at the top (the focal point). It polls
// GetIssueTimeline the same way SeriesDetail polls GetSeries so new events appear as the
// loop runs. The never-searched / empty state reuses the "Nothing here yet" empty state;
// an RPC error shows the locked server-error copy.
export function IssueTimeline({ issueId }: { issueId: bigint }) {
  const timeline = useQuery(
    getIssueTimeline,
    { issueId },
    { retry: false, refetchInterval: 5000 },
  );

  if (timeline.isError) {
    return (
      <EmptyState
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
