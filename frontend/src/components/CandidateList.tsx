import { useMutation, useQuery } from "@connectrpc/connect-query";
import { Loader2, SearchX, WifiOff } from "lucide-react";

import type { Candidate } from "../gen/omnibus/v1/search_pb";
import {
  searchIssue,
  selectCandidate,
} from "../gen/omnibus/v1/search-SearchService_connectquery";
import { CandidateRow } from "./CandidateRow";
import { EmptyState } from "./EmptyState";
import { Button } from "./ui/button";

// CandidateList runs SearchIssue for an issue and renders the ranked candidates, each with
// a "Grab" CTA wired to SelectCandidate. While the search is in flight it shows an inline
// status. When nothing cleared the acceptance floor it shows the "No acceptable releases"
// state with the human floor reason (D-04). After a successful grab it calls onGrabbed.
export function CandidateList({
  issueId,
  enabled,
  onGrabbed,
}: {
  issueId: bigint;
  enabled: boolean;
  onGrabbed?: () => void;
}) {
  const search = useQuery(searchIssue, { issueId }, { enabled, retry: false });

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
      <div className="flex items-center gap-2.5 rounded-lg border border-border bg-card/60 px-4 py-3 text-sm text-muted-foreground">
        <Loader2 className="size-4 animate-spin text-primary" />
        Searching indexers… this updates automatically.
      </div>
    );
  }

  if (search.isError) {
    return (
      <EmptyState
        icon={<WifiOff />}
        heading="Couldn't reach the server"
        body="Couldn't reach the server. Check your connection, then try again."
        cta={
          <Button variant="secondary" onClick={() => search.refetch()}>
            Retry
          </Button>
        }
      />
    );
  }

  if (search.isSuccess && !search.data.acceptable) {
    return (
      <EmptyState
        icon={<SearchX />}
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
        <p className="rounded-lg border border-tn-red/30 bg-tn-red/10 px-4 py-2.5 text-sm text-tn-red">
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
