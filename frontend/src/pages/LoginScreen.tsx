import { Code, ConnectError } from "@connectrpc/connect";
import { useMutation } from "@connectrpc/connect-query";
import { useState } from "react";

import { Button } from "../components/ui/button";
import { Input } from "../components/ui/input";
import { login } from "../gen/omnibus/v1/auth-AuthService_connectquery";
import { useAuth } from "../lib/auth";

// LogoMark is the three-spine wordmark glyph, copied from Sidebar.tsx so the Login screen
// shares the brand without importing nav chrome (the login screen has no sidebar/topbar).
function LogoMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" className={className} aria-hidden="true" fill="none">
      <rect x="3" y="5" width="4.2" height="14" rx="1.4" fill="#7aa2f7" />
      <rect x="9.4" y="3" width="4.2" height="16" rx="1.4" fill="#bb9af7" />
      <rect
        x="15.8"
        y="6.5"
        width="4.2"
        height="12.5"
        rx="1.4"
        fill="#7dcfff"
        transform="rotate(8 17.9 12.75)"
      />
    </svg>
  );
}

// The single generic credential error (D-04, T-6-32): never reveal which field was wrong.
const BAD_CREDS = "Incorrect username or password.";
// The unreachable-server copy (UI-SPEC), surfaced when Login fails for a non-auth reason.
const UNREACHABLE = "Couldn't reach the server. Check your connection, then try again.";

const fieldLabel = "flex flex-col gap-1.5 text-sm font-medium text-foreground";

// LoginScreen is the full-page sign-in shown when auth is enabled and there is no valid
// session (D-07). It short-circuits the AppShell (App.tsx renders this INSTEAD of the shell),
// so it carries no sidebar/topbar. It drives the generated AuthService Login RPC (UI-01); on
// success it marks the session authed so the gate falls through to the shell.
export function LoginScreen() {
  const { markAuthed } = useAuth();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | undefined>();

  const signIn = useMutation(login, {
    onSuccess: () => {
      setError(undefined);
      markAuthed();
    },
    onError: (err) => {
      // Bad creds -> the single generic message (D-04). Anything else -> unreachable copy.
      const code = err instanceof ConnectError ? err.code : undefined;
      setError(code === Code.Unauthenticated ? BAD_CREDS : UNREACHABLE);
    },
  });

  return (
    <div className="flex min-h-screen items-center justify-center px-6 py-12">
      <form
        className="surface-sheen flex w-full max-w-sm flex-col gap-4 rounded-xl border border-border bg-card/60 p-6"
        onSubmit={(e) => {
          e.preventDefault();
          signIn.mutate({ username, password });
        }}
      >
        <div className="flex flex-col items-center gap-2 pb-2 text-center">
          <LogoMark className="size-9" />
          <h1 className="font-display text-3xl font-semibold tracking-tight text-foreground">
            Sign in to omnibus
          </h1>
          <p className="text-base text-muted-foreground">
            Enter your username and password to continue.
          </p>
        </div>

        <label className={fieldLabel}>
          Username
          <Input
            autoFocus
            autoComplete="username"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
          />
        </label>

        <label className={fieldLabel}>
          Password
          <Input
            type="password"
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </label>

        {error ? <p className="text-sm text-tn-red">{error}</p> : null}

        <Button type="submit" size="lg" className="w-full" disabled={signIn.isPending}>
          Sign in
        </Button>
      </form>
    </div>
  );
}
