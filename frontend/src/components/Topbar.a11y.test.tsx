import { createRouterTransport } from "@connectrpc/connect";
import { screen, waitFor } from "@testing-library/react";
import { axe } from "jest-axe";
import { describe, expect, it } from "vitest";

import { AuthMode, AuthService } from "../gen/omnibus/v1/auth_pb";
import { AuthProvider } from "../lib/auth";
import { renderWithProviders } from "../test/render";
import { Topbar } from "./layout/Topbar";

// Topbar.a11y consolidates the structural a11y assertions for the topbar surface (D-12):
// jest-axe toHaveNoViolations plus explicit labelled-control checks (the hamburger drawer
// trigger and the account-menu trigger both expose accessible names). NOTE: jest-axe under
// jsdom cannot evaluate color-contrast (06-RESEARCH.md Pitfall 6) — contrast is verified in
// the manual pass; structural rules (labels/roles/names) run here.
function topbarTransport(mode: AuthMode, configured: boolean) {
  return createRouterTransport(({ service }) => {
    service(AuthService, {
      getAuthConfig: () => ({ config: { mode, username: "kyle", configured } }),
      logout: () => ({}),
    } as never);
  });
}

function renderTopbar(mode: AuthMode, configured: boolean) {
  return renderWithProviders(
    <AuthProvider>
      <Topbar active="library" onNavigate={() => {}} onOpenCommand={() => {}} />
    </AuthProvider>,
    topbarTransport(mode, configured),
  );
}

describe("Topbar accessibility", () => {
  it("has no axe violations when authed (with drawer trigger + account menu)", async () => {
    const { container } = renderTopbar(AuthMode.ON, true);
    // Wait for GetAuthConfig to settle so the AccountMenu trigger renders before the audit.
    await screen.findByRole("button", { name: /account menu/i });
    expect(await axe(container)).toHaveNoViolations();
  });

  it("has no axe violations when auth is Off (no account menu)", async () => {
    const { container } = renderTopbar(AuthMode.OFF, false);
    await waitFor(() =>
      expect(screen.getByRole("button", { name: /add series/i })).toBeInTheDocument(),
    );
    expect(await axe(container)).toHaveNoViolations();
  });

  it("exposes an accessible name on the drawer hamburger trigger", () => {
    renderTopbar(AuthMode.OFF, false);
    expect(
      screen.getByRole("button", { name: /open navigation/i }),
    ).toBeInTheDocument();
  });

  it("exposes an accessible name on the account-menu trigger when authed", async () => {
    renderTopbar(AuthMode.ON, true);
    expect(
      await screen.findByRole("button", { name: /account menu/i }),
    ).toBeInTheDocument();
  });
});
