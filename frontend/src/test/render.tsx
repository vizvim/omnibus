import { createRouterTransport, type Transport } from "@connectrpc/connect";
import { TransportProvider } from "@connectrpc/connect-query";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, type RenderResult } from "@testing-library/react";
import type { ReactElement, ReactNode } from "react";

import { IndexerService } from "../gen/omnibus/v1/indexers_pb";
import { JobService } from "../gen/omnibus/v1/jobs_pb";
import { SeriesService } from "../gen/omnibus/v1/series_pb";

// SeriesServiceImpl is a partial stub of the SeriesService RPCs for component tests.
export type SeriesServiceImpl = Parameters<
  Parameters<typeof createRouterTransport>[0]
>[0]["service"] extends never
  ? never
  : Record<string, unknown>;

// makeTransport builds an in-memory Connect transport that serves the provided
// SeriesService method stubs — no network, deterministic for tests.
export function makeTransport(
  impl: Record<string, (req: never) => unknown>,
): Transport {
  return createRouterTransport(({ service }) => {
    service(SeriesService, impl as never);
  });
}

// makeJobTransport builds an in-memory Connect transport serving JobService method
// stubs (e.g. listJobRuns) for Activity-page component tests.
export function makeJobTransport(
  impl: Record<string, (req: never) => unknown>,
): Transport {
  return createRouterTransport(({ service }) => {
    service(JobService, impl as never);
  });
}

// makeIndexerTransport builds an in-memory Connect transport serving IndexerService
// method stubs (e.g. listIndexers/createIndexer) for Indexers-page component tests.
export function makeIndexerTransport(
  impl: Record<string, (req: never) => unknown>,
): Transport {
  return createRouterTransport(({ service }) => {
    service(IndexerService, impl as never);
  });
}

// renderWithProviders mounts a component inside the Connect TransportProvider +
// a fresh TanStack QueryClient bound to the given (mock) transport.
export function renderWithProviders(
  ui: ReactElement,
  transport: Transport,
): RenderResult {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });

  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <TransportProvider transport={transport}>
        <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
      </TransportProvider>
    );
  }

  return render(ui, { wrapper: Wrapper });
}
