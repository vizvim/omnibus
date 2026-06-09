import { createConnectTransport } from "@connectrpc/connect-web";
import { TransportProvider } from "@connectrpc/connect-query";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";

// Connect transport points at /api, which the Vite dev server proxies to the Go
// backend (and which is same-origin in production). The generated connect-query
// descriptors are driven through this transport + TanStack Query.
const transport = createConnectTransport({ baseUrl: "/api" });
const queryClient = new QueryClient();

export function Providers({ children }: { children: ReactNode }) {
  return (
    <TransportProvider transport={transport}>
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    </TransportProvider>
  );
}
