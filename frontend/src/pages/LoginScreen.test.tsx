import { ConnectError, Code } from "@connectrpc/connect";
import { createRouterTransport } from "@connectrpc/connect";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { axe } from "jest-axe";
import type { ReactNode } from "react";
import { describe, expect, it } from "vitest";

import { AuthProvider } from "../lib/auth";
import { AuthService } from "../gen/omnibus/v1/auth_pb";
import { renderWithProviders } from "../test/render";
import { LoginScreen } from "./LoginScreen";

// authTransport serves AuthService stubs so the Login screen's useMutation(login) and the
// AuthProvider's GetAuthConfig query resolve in-memory (no network). getAuthConfig defaults
// to an empty stub so the wrapping AuthProvider's query settles.
function authTransport(impl: Record<string, (req: never) => unknown>) {
  return createRouterTransport(({ service }) => {
    service(AuthService, { getAuthConfig: () => ({}), ...impl } as never);
  });
}

// withAuth wraps the LoginScreen in the AuthProvider it consumes via useAuth().
function withAuth(node: ReactNode) {
  return <AuthProvider>{node}</AuthProvider>;
}

describe("LoginScreen", () => {
  it("renders the heading, body, and the locked field labels", () => {
    const transport = authTransport({ login: () => ({}) });
    renderWithProviders(withAuth(<LoginScreen />), transport);

    expect(screen.getByRole("heading", { name: /sign in to omnibus/i })).toBeInTheDocument();
    expect(
      screen.getByText(/enter your username and password to continue/i),
    ).toBeInTheDocument();
    expect(screen.getByLabelText(/username/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/password/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /sign in/i })).toBeInTheDocument();
  });

  it("submits credentials through the generated Login RPC", async () => {
    let received: { username: string; password: string } | undefined;
    const transport = authTransport({
      login: (req: never) => {
        received = req as { username: string; password: string };
        return {};
      },
    });
    renderWithProviders(withAuth(<LoginScreen />), transport);

    fireEvent.change(screen.getByLabelText(/username/i), { target: { value: "kyle" } });
    fireEvent.change(screen.getByLabelText(/password/i), { target: { value: "hunter2" } });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));

    await waitFor(() => expect(received).toBeDefined());
    expect(received?.username).toBe("kyle");
    expect(received?.password).toBe("hunter2");
  });

  it("shows the single generic error on bad credentials (no field-specific hint)", async () => {
    const transport = authTransport({
      login: () => {
        throw new ConnectError("nope", Code.Unauthenticated);
      },
    });
    renderWithProviders(withAuth(<LoginScreen />), transport);

    fireEvent.change(screen.getByLabelText(/username/i), { target: { value: "kyle" } });
    fireEvent.change(screen.getByLabelText(/password/i), { target: { value: "wrong" } });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));

    expect(await screen.findByText("Incorrect username or password.")).toBeInTheDocument();
  });

  it("shows the unreachable-server copy on a transport failure", async () => {
    const transport = authTransport({
      login: () => {
        throw new ConnectError("down", Code.Unavailable);
      },
    });
    renderWithProviders(withAuth(<LoginScreen />), transport);

    fireEvent.change(screen.getByLabelText(/username/i), { target: { value: "kyle" } });
    fireEvent.change(screen.getByLabelText(/password/i), { target: { value: "hunter2" } });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));

    expect(
      await screen.findByText(/couldn't reach the server\. check your connection/i),
    ).toBeInTheDocument();
  });

  it("orders focus username -> password -> sign in", () => {
    const transport = authTransport({ login: () => ({}) });
    renderWithProviders(withAuth(<LoginScreen />), transport);

    const username = screen.getByLabelText(/username/i);
    const password = screen.getByLabelText(/password/i);
    const submit = screen.getByRole("button", { name: /sign in/i });

    // Logical DOM order: username precedes password precedes the submit button.
    expect(username.compareDocumentPosition(password)).toBe(
      Node.DOCUMENT_POSITION_FOLLOWING,
    );
    expect(password.compareDocumentPosition(submit)).toBe(
      Node.DOCUMENT_POSITION_FOLLOWING,
    );
  });

  it("has no axe violations", async () => {
    const transport = authTransport({ login: () => ({}) });
    const { container } = renderWithProviders(withAuth(<LoginScreen />), transport);
    expect(await axe(container)).toHaveNoViolations();
  });
});
