import { useMutation, useQuery } from "@connectrpc/connect-query";

import type { Candidate } from "../gen/omnibus/v1/search_pb";
import {
  searchIssue,
  selectCandidate,
} from "../gen/omnibus/v1/search-SearchService_connectquery";
import { CandidateRow } from "./CandidateRow";
import { EmptyState } from "./EmptyState";

// CandidateList runs SearchIssue for an issue and renders the ranked candidates, each
// with a "Grab" CTA wired to SelectCandidate. While the search is in flight it shows the
// locked inline status. When the search ran but nothing cleared the acceptance floor
// (acceptable=false) it shows the "No acceptable releases" empty state and surfaces the
// human floor reason (D-04). On a SelectCandidate failure it shows the locked grab-error
// copy. After a successful grab it calls onGrabbed so the parent can refresh the timeline.
export function CandidateList({
  issueId,
  enabled,
  onGrabbed,
}: {
  issueId: bigint;
  enabled: boolean;
  onGrabbed?: () => void;
}) {
  const search = useQuery(
    searchIssue,
    { issueId },
    { enabled, retry: false },
  );

  const grab = useMutation(selectCandidate, {
    onSuccess: () => onGrabbed?.(),
  });

  function handleGrab(candidate: Candidate) {
    grab.mutate({
      issueId,
      provider: candidate.provider,
      releaseKey: candidate.releaseKey,
    });
  }

  if (!enabled) return null;

  if (search.isFetching) {
    return (
      <p className="text-sm text-slate-600">
        Searching indexers… this updates automatically.
      </p>
    );
  }

  if (search.isError) {
    return (
      <EmptyState
        heading="Couldn't reach the server"
        body="Couldn't reach the server. Check your connection, then try again."
        cta={
          <button
            type="button"
            onClick={() => search.refetch()}
            className="rounded bg-blue-600 px-4 py-2 text-sm font-semibold text-white"
          >
            Retry
          </button>
        }
      />
    );
  }

  if (search.isSuccess && !search.data.acceptable) {
    return (
      <EmptyState
        heading="No acceptable releases"
        body={
          search.data.floorReason
            ? `No release cleared the match and quality checks for this issue (${search.data.floorReason}). omnibus will keep trying automatically, or try again later.`
            : "No release cleared the match and quality checks for this issue. omnibus will keep trying automatically, or try again later."
        }
      />
    );
  }

  return (
    <div className="flex flex-col gap-2">
      {grab.isError ? (
        <p className="text-sm text-red-600">
          Couldn't hand this release off to the download client. Check the client is
          reachable, then try again.
        </p>
      ) : null}
      {search.data?.candidates.map((candidate) => (
        <CandidateRow
          key={`${candidate.provider}:${candidate.releaseKey}`}
          candidate={candidate}
          onGrab={handleGrab}
          grabbing={grab.isPending}
        />
      ))}
    </div>
  );
}
