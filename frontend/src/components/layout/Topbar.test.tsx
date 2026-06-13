import { createRouterTransport } from "@connectrpc/connect";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { AuthMode, AuthService } from "../../gen/omnibus/v1/auth_pb";
import { AuthProvider } from "../../lib/auth";
import { renderWithProviders } from "../../test/render";
import { Topbar } from "./Topbar";

// topbarTransport serves the AuthService stubs the AuthProvider + AccountMenu need.
function topbarTransport(
  mode: AuthMode,
  configured: boolean,
  onLogout?: () => void,
) {
  return createRouterTransport(({ service }) => {
    service(AuthService, {
      getAuthConfig: () => ({ config: { mode, username: "kyle", configured } }),
      logout: () => {
        onLogout?.();
        return {};
      },
    } as never);
  });
}

function renderTopbar(transport: ReturnType<typeof topbarTransport>) {
  return renderWithProviders(
    <AuthProvider>
      <Topbar active="library" onNavigate={() => {}} onOpenCommand={() => {}} />
    </AuthProvider>,
    transport,
  );
}

describe("Topbar account menu", () => {
  it("hides the account menu when auth is Off", async () => {
    renderTopbar(topbarTransport(AuthMode.OFF, false));
    // Let GetAuthConfig settle, then assert the trigger never appears.
    await waitFor(() =>
      expect(screen.getByRole("button", { name: /add series/i })).toBeInTheDocument(),
    );
    expect(screen.queryByRole("button", { name: /account menu/i })).toBeNull();
  });

  it("shows the account menu when auth is On (enabled + authed)", async () => {
    renderTopbar(topbarTransport(AuthMode.ON, true));
    expect(
      await screen.findByRole("button", { name: /account menu/i }),
    ).toBeInTheDocument();
  });

  it("clears the session via the Logout RPC on Sign out", async () => {
    let loggedOut = false;
    renderTopbar(topbarTransport(AuthMode.ON, true, () => (loggedOut = true)));

    // Open the Radix dropdown via keyboard (reliable in jsdom), then select Sign out.
    const trigger = await screen.findByRole("button", { name: /account menu/i });
    fireEvent.keyDown(trigger, { key: "Enter" });
    fireEvent.click(await screen.findByRole("menuitem", { name: /sign out/i }));

    await waitFor(() => expect(loggedOut).toBe(true));
  });
});
