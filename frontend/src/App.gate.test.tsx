import { createRouterTransport } from "@connectrpc/connect";
import { act, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { App } from "./App";
import { AuthMode, AuthService } from "./gen/omnibus/v1/auth_pb";
import { SeriesService } from "./gen/omnibus/v1/series_pb";
import { AuthProvider, notifyUnauthorized } from "./lib/auth";
import { renderWithProviders } from "./test/render";

// gateTransport serves the AuthService (for the AuthProvider's GetAuthConfig) plus a minimal
// SeriesService (Library's listSeries) so the shell route can mount without hanging.
function gateTransport(mode: AuthMode, configured = true) {
  return createRouterTransport(({ service }) => {
    service(AuthService, {
      getAuthConfig: () => ({ config: { mode, username: "kyle", configured } }),
      login: () => ({}),
    } as never);
    service(SeriesService, { listSeries: () => ({ series: [] }) } as never);
  });
}

describe("App auth gate", () => {
  it("renders the AppShell (not Login) when auth is Off", async () => {
    const transport = gateTransport(AuthMode.OFF);
    renderWithProviders(
      <AuthProvider>
        <App initialRoute="library" />
      </AuthProvider>,
      transport,
    );

    // The library route's empty state proves the shell rendered; no Login heading.
    await waitFor(() =>
      expect(screen.getByText(/no series in your library yet/i)).toBeInTheDocument(),
    );
    expect(screen.queryByRole("heading", { name: /sign in to omnibus/i })).toBeNull();
  });

  it("renders the AppShell when auth is On and the session is valid (no 401)", async () => {
    const transport = gateTransport(AuthMode.ON);
    renderWithProviders(
      <AuthProvider>
        <App initialRoute="library" />
      </AuthProvider>,
      transport,
    );

    await waitFor(() =>
      expect(screen.getByText(/no series in your library yet/i)).toBeInTheDocument(),
    );
    expect(screen.queryByRole("heading", { name: /sign in to omnibus/i })).toBeNull();
  });

  it("short-circuits to the full-page Login when On and a 401 is signalled", async () => {
    const transport = gateTransport(AuthMode.ON);
    renderWithProviders(
      <AuthProvider>
        <App initialRoute="library" />
      </AuthProvider>,
      transport,
    );

    // Wait for GetAuthConfig to report mode=On (enabled). The shell renders first
    // (optimistically authed); then a 401 from any RPC engages the gate.
    await waitFor(() =>
      expect(screen.getByText(/no series in your library yet/i)).toBeInTheDocument(),
    );

    act(() => notifyUnauthorized());

    expect(
      await screen.findByRole("heading", { name: /sign in to omnibus/i }),
    ).toBeInTheDocument();
    // The shell is gone — never a partially-authed shell (D-04, T-6-30).
    expect(screen.queryByText(/no series in your library yet/i)).toBeNull();
  });
});
