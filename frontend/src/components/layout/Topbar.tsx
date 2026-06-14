import { useMutation } from "@connectrpc/connect-query";
import { LogOut, Plus, Search, UserRound } from "lucide-react";

import { logout } from "../../gen/omnibus/v1/auth-AuthService_connectquery";
import { useAuthOptional } from "../../lib/auth";
import { Button } from "../ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "../ui/dropdown-menu";
import { ConnectionState } from "./ConnectionState";
import { MobileNavDrawer } from "./MobileNavDrawer";
import type { Navigate, Route } from "./nav";

// AccountMenu is the topbar account/Sign-out affordance, rendered only when auth is enabled
// AND the session is active (D-07). Sign-out is non-destructive (no blocking confirm): it
// clears the session via the generated Logout RPC, then signals unauthenticated so the App
// gate short-circuits back to the Login screen.
function AccountMenu() {
  const auth = useAuthOptional();
  const signOut = useMutation(logout, {
    // Whether or not the RPC succeeds, drop the local session so the gate engages.
    onSettled: () => auth?.signalUnauthenticated(),
  });

  // Render nothing unless auth is enabled AND the session is active (D-07), and only when an
  // AuthProvider is actually mounted (provider-less component tests render the shell alone).
  if (!auth || !auth.enabled || !auth.authed) {
    return null;
  }
  const { username } = auth;

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="icon"
          aria-label="Account menu"
          // Ensure a >=44px touch target on coarse pointers (UI-SPEC), padding the hit area
          // without shrinking the visible chrome.
          className="size-11 sm:size-9"
        >
          <UserRound className="size-4" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {username ? <DropdownMenuLabel>{username}</DropdownMenuLabel> : null}
        {username ? <DropdownMenuSeparator /> : null}
        <DropdownMenuItem onSelect={() => signOut.mutate({})}>
          <LogOut className="size-4" />
          Sign out
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

export function Topbar({
  active,
  onNavigate,
  onOpenCommand,
}: {
  active: Route;
  onNavigate: Navigate;
  onOpenCommand: () => void;
}) {
  return (
    <header className="sticky top-0 z-30 flex h-16 items-center gap-3 border-b border-border bg-background/70 px-5 backdrop-blur-xl md:px-8">
      {/* mobile nav: hamburger -> focus-trapped left drawer (below lg only, hidden at lg+) */}
      <MobileNavDrawer active={active} onNavigate={onNavigate} onOpenCommand={onOpenCommand} />

      {/* command launcher — styled as a search field, opens ⌘K */}
      <button
        type="button"
        onClick={onOpenCommand}
        className="group flex h-9 w-full max-w-md items-center gap-2.5 rounded-lg border border-border bg-card/60 px-3 text-sm text-muted-foreground transition-colors hover:border-border-strong hover:bg-card"
      >
        <Search className="size-4 shrink-0 transition-colors group-hover:text-foreground" />
        <span className="flex-1 truncate text-left">Search library or ComicVine…</span>
        <kbd className="hidden shrink-0 rounded border border-border-strong/70 bg-secondary px-1.5 py-0.5 font-mono text-[0.65rem] text-muted-foreground sm:inline">
          ⌘K
        </kbd>
      </button>

      <div className="ml-auto flex items-center gap-2">
        {/* Live-status indicator bound to the unified stream (UI-05): replaces the
            hardcoded "online" dot with live/reconnecting/offline (D-08/D-09). */}
        <ConnectionState />
        <Button onClick={() => onNavigate("search")} className="gap-1.5">
          <Plus className="size-4" />
          <span className="hidden sm:inline">Add series</span>
        </Button>
        <AccountMenu />
      </div>
    </header>
  );
}
