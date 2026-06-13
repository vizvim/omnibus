import { createConnectTransport } from "@connectrpc/connect-web";
import { TransportProvider } from "@connectrpc/connect-query";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createContext, useContext, type ReactNode } from "react";

import { type ConnectionState, useEventStream } from "./useEventStream";

// Connect transport points at /api, which the Vite dev server proxies to the Go
// backend (and which is same-origin in production). The generated connect-query
// descriptors are driven through this transport + TanStack Query. Same-origin means the
// session cookie flows without `credentials:"include"` (06-RESEARCH.md Pitfall 2 / A3).
const transport = createConnectTransport({ baseUrl: "/api" });
const queryClient = new QueryClient();

// ConnectionStateContext carries the single root stream's live/reconnecting/offline state so
// the topbar ConnectionState indicator (and any polling-fallback logic) can read it without
// re-subscribing. Defaults to "reconnecting" until the root subscription reports otherwise.
const ConnectionStateContext = createContext<ConnectionState>("reconnecting");

// useConnectionState reads the live stream's connection health from context.
export function useConnectionState(): ConnectionState {
  return useContext(ConnectionStateContext);
}

// StreamSubscription mounts the unified live-status stream ONCE (D-08) and publishes its
// connection state to context. It renders its children unchanged. It lives inside the
// transport + query providers so the hook has both in scope.
function StreamSubscription({ children }: { children: ReactNode }) {
  const state = useEventStream();
  return (
    <ConnectionStateContext.Provider value={state}>
      {children}
    </ConnectionStateContext.Provider>
  );
}

export function Providers({ children }: { children: ReactNode }) {
  return (
    <TransportProvider transport={transport}>
      <QueryClientProvider client={queryClient}>
        <StreamSubscription>{children}</StreamSubscription>
      </QueryClientProvider>
    </TransportProvider>
  );
}
