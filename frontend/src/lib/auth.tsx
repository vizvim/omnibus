import { useQuery } from "@connectrpc/connect-query";
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";

import { getAuthConfig } from "../gen/omnibus/v1/auth-AuthService_connectquery";
import { AuthMode } from "../gen/omnibus/v1/auth_pb";

// A module-level fan-out so the Connect transport interceptor (created outside React, see
// transport.tsx) can notify the React AuthProvider when any RPC returns Unauthenticated.
// This is the "treat a 401 from any RPC as not-authed" signal that engages the gate.
type UnauthorizedListener = () => void;
const unauthorizedListeners = new Set<UnauthorizedListener>();

// notifyUnauthorized is called by the transport interceptor on a 401; it fans out to any
// mounted AuthProvider so the gate short-circuits to the Login screen.
export function notifyUnauthorized(): void {
  for (const listener of unauthorizedListeners) {
    listener();
  }
}

function subscribeUnauthorized(listener: UnauthorizedListener): () => void {
  unauthorizedListeners.add(listener);
  return () => {
    unauthorizedListeners.delete(listener);
  };
}

// AuthContextValue is what useAuth() exposes to the gate (App.tsx), the Login screen, the
// Settings auth card, and the topbar logout affordance.
export interface AuthContextValue {
  // enabled is true when the gate is On or On-except-local (mode !== Off/Unspecified). It
  // drives the App short-circuit together with `authed`.
  enabled: boolean;
  // configured is true when a credential is stored server-side (never the secret itself).
  configured: boolean;
  // username is the single-user login name (no secret).
  username: string;
  // authed tracks whether the current session is valid. It starts optimistically `true` so
  // the AppShell renders by default (the server middleware is the real boundary, D-04 /
  // T-6-30: this UI gate is defense-in-depth). Any Unauthenticated RPC flips it to `false`
  // via signalUnauthenticated(), engaging the Login screen.
  authed: boolean;
  // loading is true while the initial GetAuthConfig query is in flight.
  loading: boolean;
  // markAuthed flips authed back to true after a successful Login and refetches the config.
  markAuthed: () => void;
  // signalUnauthenticated flips authed to false when any RPC reports a 401 (no/invalid
  // session), so the gate short-circuits to the Login screen.
  signalUnauthenticated: () => void;
  // refresh refetches GetAuthConfig (e.g. after the Settings auth card saves a new mode).
  refresh: () => void;
}

const AuthContext = createContext<AuthContextValue | undefined>(undefined);

// useAuth reads the auth context. It must be used inside <AuthProvider>.
export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (ctx === undefined) {
    throw new Error("useAuth must be used within an AuthProvider");
  }
  return ctx;
}

// useAuthGate reads the auth gate state without requiring an AuthProvider: when none is
// mounted (e.g. focused component tests that only stub a single service) it falls open to
// the shell — never to a spurious Login screen. The server middleware remains the real
// boundary (D-04 / T-6-30), so a missing client-side provider can only fail open, not leak.
export function useAuthGate(): Pick<AuthContextValue, "enabled" | "authed"> {
  const ctx = useContext(AuthContext);
  if (ctx === undefined) {
    return { enabled: false, authed: true };
  }
  return { enabled: ctx.enabled, authed: ctx.authed };
}

// AuthProvider loads GetAuthConfig (via the generated connect-query hook) and tracks session
// state. It does NOT change the transport — the same-origin cookie flows without
// `credentials:"include"` (06-RESEARCH.md Pitfall 2). It mirrors transport.tsx's Provider
// shape (a context Provider wrapping children).
export function AuthProvider({ children }: { children: ReactNode }) {
  // retry:false so a 401/unreachable config doesn't spin; the gate falls open to the shell
  // when the config is unknown (the server middleware still enforces, D-04).
  const config = useQuery(getAuthConfig, {}, { retry: false });

  const [unauthed, setUnauthed] = useState(false);

  const markAuthed = useCallback(() => {
    setUnauthed(false);
    void config.refetch();
  }, [config]);

  const signalUnauthenticated = useCallback(() => {
    setUnauthed(true);
  }, []);

  // Engage the gate when any RPC reports a 401 via the transport interceptor (module-level
  // fan-out, since the transport is constructed outside React).
  useEffect(() => subscribeUnauthorized(signalUnauthenticated), [signalUnauthenticated]);

  const refresh = useCallback(() => {
    void config.refetch();
  }, [config]);

  const cfg = config.data?.config;
  const mode = cfg?.mode ?? AuthMode.UNSPECIFIED;
  const enabled = mode === AuthMode.ON || mode === AuthMode.BYPASS_LOCAL;

  const value = useMemo<AuthContextValue>(
    () => ({
      enabled,
      configured: cfg?.configured ?? false,
      username: cfg?.username ?? "",
      authed: !unauthed,
      loading: config.isLoading,
      markAuthed,
      signalUnauthenticated,
      refresh,
    }),
    [
      enabled,
      cfg?.configured,
      cfg?.username,
      unauthed,
      config.isLoading,
      markAuthed,
      signalUnauthenticated,
      refresh,
    ],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}
