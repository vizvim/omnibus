import { createClient } from "@connectrpc/connect";
import { createConnectQueryKey, useTransport } from "@connectrpc/connect-query";
import { type QueryClient, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";

import { listJobRuns } from "../gen/omnibus/v1/jobs-JobService_connectquery";
import { getIssueTimeline } from "../gen/omnibus/v1/search-SearchService_connectquery";
import { getSeries } from "../gen/omnibus/v1/series-SeriesService_connectquery";
import { EventService } from "../gen/omnibus/v1/events_pb";
import type { EventEnvelope } from "../gen/omnibus/v1/events_pb";

// ConnectionState is the live-status health the ConnectionState pill reflects (UI-05):
// "live" while the stream is flowing, "reconnecting" between drops/backoff, "offline" once
// the subscription is stopped/aborted.
export type ConnectionState = "live" | "reconnecting" | "offline";

// EventStreamClient is the narrow subset of the generated EventService client the loop needs
// — just the server-streaming method. Declaring it lets tests inject a fake async-iterable.
export interface EventStreamClient {
  streamEvents: (
    req?: { signal?: AbortSignal } | unknown,
    opts?: { signal?: AbortSignal },
  ) => AsyncIterable<EventEnvelope>;
}

// invalidate keys for the views the stream drives. Omitting `input` matches every input for
// the method, so a single event refreshes all mounted instances (e.g. any open SeriesDetail).
function seriesKey() {
  return createConnectQueryKey({ schema: getSeries, cardinality: "finite" });
}
function timelineKey() {
  return createConnectQueryKey({ schema: getIssueTimeline, cardinality: "finite" });
}
function jobsKey() {
  return createConnectQueryKey({ schema: listJobRuns, cardinality: "finite" });
}

// fanOut maps one streamed envelope onto the existing TanStack Query caches (D-08/D-10). It
// invalidates the matching query keys so the live views refetch/patch without a manual
// refresh. Switching on the oneof `case` keeps the mapping exhaustive and typed.
function fanOut(queryClient: QueryClient, env: EventEnvelope): void {
  switch (env.event.case) {
    case "issueStatus":
      // An issue's lifecycle changed: refresh its series (issue grid) + timeline.
      void queryClient.invalidateQueries({ queryKey: seriesKey() });
      void queryClient.invalidateQueries({ queryKey: timelineKey() });
      break;
    case "downloadProgress":
      // Download progress/terminal status: refresh the timeline + the series header.
      void queryClient.invalidateQueries({ queryKey: timelineKey() });
      void queryClient.invalidateQueries({ queryKey: seriesKey() });
      break;
    case "timeline":
      // A new per-issue timeline event: refresh the timeline view.
      void queryClient.invalidateQueries({ queryKey: timelineKey() });
      break;
    case "jobState":
      // A background job changed state: refresh the Activity job-run list.
      void queryClient.invalidateQueries({ queryKey: jobsKey() });
      break;
    default:
      // Unknown/empty oneof: nothing to fan out.
      break;
  }
}

// sleep resolves after ms, or immediately if the signal is already aborted.
function sleep(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve) => {
    if (signal?.aborted) {
      resolve();
      return;
    }
    const t = setTimeout(resolve, ms);
    signal?.addEventListener("abort", () => {
      clearTimeout(t);
      resolve();
    });
  });
}

// RunEventStreamOptions parameterizes the reconnect loop for both production and tests.
export interface RunEventStreamOptions {
  client: EventStreamClient;
  queryClient: QueryClient;
  signal: AbortSignal;
  onState: (state: ConnectionState) => void;
  // backoffMs returns the delay before the Nth (0-based) reconnect attempt. Defaults to
  // capped exponential backoff (D-09). Tests pass a 0 backoff for determinism.
  backoffMs?: (attempt: number) => number;
}

// defaultBackoff is capped exponential backoff: 500ms, 1s, 2s, 4s, … up to 30s.
function defaultBackoff(attempt: number): number {
  return Math.min(30_000, 500 * 2 ** attempt);
}

// runEventStream is the subscription + reconnect + cache fan-out loop (D-08/D-09). It opens
// the server stream, fans each envelope into the caches, and on a drop reconnects with
// backoff — invalidating active queries on every (re)connect so the UI converges after a gap.
// It returns when the signal aborts. Exported (rather than inlined in the hook) so it is
// unit-testable with a fake async-iterable client.
export async function runEventStream(opts: RunEventStreamOptions): Promise<void> {
  const { client, queryClient, signal, onState } = opts;
  const backoffMs = opts.backoffMs ?? defaultBackoff;
  let failures = 0;

  while (!signal.aborted) {
    try {
      // On every (re)connect, invalidate the live queries so the UI converges after any gap
      // between the last received event and this connection (D-09).
      void queryClient.invalidateQueries({ queryKey: seriesKey() });
      void queryClient.invalidateQueries({ queryKey: timelineKey() });
      void queryClient.invalidateQueries({ queryKey: jobsKey() });

      onState("live");
      failures = 0;

      for await (const env of client.streamEvents({}, { signal })) {
        if (signal.aborted) break;
        fanOut(queryClient, env);
      }

      // The stream ended without an error (server closed or signal aborted).
      if (signal.aborted) break;
    } catch {
      // A dropped/failed stream — fall through to the backoff + reconnect below.
      if (signal.aborted) break;
    }

    // Reconnect with backoff (D-09). Surface the reconnecting state for the indicator.
    onState("reconnecting");
    await sleep(backoffMs(failures), signal);
    failures += 1;
  }

  onState("offline");
}

// useEventStream is the single root-mounted subscription hook (D-08): it subscribes ONCE via
// the raw connect-es async-iterable client (NOT connect-query useQuery — unary only) and fans
// events into the existing caches with a resilient reconnect path. It exposes the live
// connection state for the ConnectionState indicator.
export function useEventStream(): ConnectionState {
  const transport = useTransport();
  const queryClient = useQueryClient();
  const [state, setState] = useState<ConnectionState>("reconnecting");

  useEffect(() => {
    const controller = new AbortController();
    const client = createClient(EventService, transport) as unknown as EventStreamClient;

    void runEventStream({
      client,
      queryClient,
      signal: controller.signal,
      onState: setState,
    });

    return () => controller.abort();
  }, [transport, queryClient]);

  return state;
}
