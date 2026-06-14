import { QueryClient } from "@tanstack/react-query";
import { waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { EventEnvelope } from "../gen/omnibus/v1/events_pb";
import { runEventStream } from "./useEventStream";

// makeEnvelope is a tiny helper to build a typed envelope for a given oneof case.
function jobStateEnvelope(kind: string, state: string): EventEnvelope {
  return {
    $typeName: "omnibus.v1.EventEnvelope",
    occurredAt: "2026-06-13T00:00:00Z",
    event: {
      case: "jobState",
      value: { $typeName: "omnibus.v1.JobStateEvent", kind, state },
    },
  } as EventEnvelope;
}

function issueStatusEnvelope(issueId: bigint): EventEnvelope {
  return {
    $typeName: "omnibus.v1.EventEnvelope",
    occurredAt: "2026-06-13T00:00:00Z",
    event: {
      case: "issueStatus",
      value: { $typeName: "omnibus.v1.IssueStatusEvent", issueId, status: 3 },
    },
  } as EventEnvelope;
}

// fakeStreamOnce yields the given envelopes then completes (the connection then ends).
async function* fakeStreamOnce(envs: EventEnvelope[]): AsyncIterable<EventEnvelope> {
  for (const env of envs) {
    yield env;
  }
}

describe("runEventStream", () => {
  it("fans an event into the query cache (invalidate on issueStatus)", async () => {
    const queryClient = new QueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");

    const states: string[] = [];
    const controller = new AbortController();

    const client = {
      // One connect: yield an issueStatus event, then end; abort so we don't reconnect.
      streamEvents: vi.fn(() => {
        controller.abort();
        return fakeStreamOnce([issueStatusEnvelope(7n)]);
      }),
    };

    await runEventStream({
      client,
      queryClient,
      signal: controller.signal,
      onState: (s) => states.push(s),
      backoffMs: () => 0,
    });

    // An issueStatus event invalidates the active live queries so the UI converges.
    expect(invalidate).toHaveBeenCalled();
    // The hook reported a live state at least once before it stopped.
    expect(states).toContain("live");
  });

  it("reconnects with backoff on stream error and converges via invalidate", async () => {
    const queryClient = new QueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const states: string[] = [];
    const controller = new AbortController();

    let attempt = 0;
    const client = {
      streamEvents: vi.fn(() => {
        attempt += 1;
        if (attempt === 1) {
          // First connection drops with an error.
          // eslint-disable-next-line require-yield
          return (async function* () {
            throw new Error("stream dropped");
          })();
        }
        // Second connection: deliver one event then stop the loop.
        controller.abort();
        return fakeStreamOnce([jobStateEnvelope("import_series", "running")]);
      }),
    };

    await runEventStream({
      client,
      queryClient,
      signal: controller.signal,
      onState: (s) => states.push(s),
      backoffMs: () => 0,
    });

    // It retried after the drop (two connect attempts).
    expect(client.streamEvents).toHaveBeenCalledTimes(2);
    // It surfaced a reconnecting state between attempts (D-09 resilience).
    expect(states).toContain("reconnecting");
    // On (re)connect it invalidates active queries so the UI converges after the gap.
    await waitFor(() => expect(invalidate).toHaveBeenCalled());
  });
});
