import * as DialogPrimitive from "@radix-ui/react-dialog";
import { Command as CommandIcon, Menu } from "lucide-react";
import { useState } from "react";

import { cn } from "../../lib/utils";
import {
  Dialog,
  DialogOverlay,
  DialogPortal,
  DialogTitle,
  DialogTrigger,
} from "../ui/dialog";
import { NAV_ITEMS, type Navigate, type Route } from "./nav";

// DrawerNavButton mirrors the Sidebar's NavButton markup (active accent rail + icon + label)
// but always shows the label — the drawer is the mobile labelled-nav, not the icon rail. It
// renders one entry from the SHARED NAV_ITEMS source (no forked list, D-13).
function DrawerNavButton({
  item,
  active,
  onClick,
}: {
  item: (typeof NAV_ITEMS)[number];
  active: boolean;
  onClick: () => void;
}) {
  const Icon = item.icon;
  return (
    <button
      type="button"
      onClick={onClick}
      aria-current={active ? "page" : undefined}
      className={cn(
        // >=44px touch target on coarse pointers (h-11) — UI-SPEC mobile targets.
        "group relative flex h-11 w-full items-center gap-3 rounded-lg px-3 text-sm font-medium transition-all duration-150",
        active
          ? "bg-secondary text-foreground"
          : "text-muted-foreground hover:bg-secondary/60 hover:text-foreground",
      )}
    >
      <span
        className={cn(
          "absolute left-0 top-1/2 h-5 w-[3px] -translate-y-1/2 rounded-r-full bg-primary transition-all duration-200",
          active ? "opacity-100 shadow-[0_0_10px_0_rgba(122,162,247,0.8)]" : "opacity-0",
        )}
        aria-hidden="true"
      />
      <Icon
        className={cn(
          "size-[18px] shrink-0 transition-colors",
          active ? "text-primary" : "text-muted-foreground group-hover:text-foreground",
        )}
      />
      <span className="truncate">{item.label}</span>
    </button>
  );
}

// MobileNavDrawer is the below-`lg` navigation: a hamburger trigger in the topbar opens a
// left-anchored Radix Dialog drawer (focus-trapped, ESC-closes, focus restored to the trigger
// on close — all free from the Radix primitive, UI-04). It renders the SAME NAV_ITEMS the
// persistent Sidebar uses (D-13: do not fork a second nav). At `lg+` the trigger is hidden
// (`lg:hidden`) and the persistent Sidebar is the nav instead. Selecting an item navigates and
// closes the drawer. An optional command launcher mirrors the sidebar's ⌘K affordance.
export function MobileNavDrawer({
  active,
  onNavigate,
  onOpenCommand,
}: {
  active: Route;
  onNavigate: Navigate;
  onOpenCommand?: () => void;
}) {
  const [open, setOpen] = useState(false);

  // navigate + close so the drawer dismisses on selection (Radix then restores focus).
  const go = (route: (typeof NAV_ITEMS)[number]["route"]) => {
    onNavigate(route);
    setOpen(false);
  };

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <button
          type="button"
          aria-label="Open navigation"
          // Shown only below lg (the persistent Sidebar covers lg+). >=44px touch target.
          className="flex size-11 shrink-0 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-secondary/60 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50 lg:hidden"
        >
          <Menu className="size-5" />
        </button>
      </DialogTrigger>
      <DialogPortal>
        <DialogOverlay />
        <DialogPrimitive.Content
          // Left-anchored drawer panel (overrides the centered dialog defaults).
          className="surface-sheen fixed inset-y-0 left-0 z-50 flex w-[min(18rem,85vw)] flex-col border-r border-border-strong/60 bg-popover px-3 py-4 shadow-2xl data-[state=open]:animate-overlay"
        >
          <DialogTitle className="px-2 pb-3 font-display text-lg font-semibold tracking-tight">
            omnibus
          </DialogTitle>
          <nav className="flex flex-col gap-1">
            {NAV_ITEMS.map((item) => (
              <DrawerNavButton
                key={item.route}
                item={item}
                active={active === item.route}
                onClick={() => go(item.route)}
              />
            ))}
          </nav>
          {onOpenCommand ? (
            <button
              type="button"
              onClick={() => {
                setOpen(false);
                onOpenCommand();
              }}
              className="mt-auto flex h-11 items-center gap-3 rounded-lg border border-border bg-tn-night/40 px-3 text-sm text-muted-foreground transition-colors hover:border-border-strong hover:text-foreground"
            >
              <CommandIcon className="size-[18px] shrink-0" />
              <span className="flex-1 text-left">Command</span>
              <kbd className="rounded border border-border-strong/70 bg-secondary px-1.5 font-mono text-[0.65rem] text-muted-foreground">
                ⌘K
              </kbd>
            </button>
          ) : null}
        </DialogPrimitive.Content>
      </DialogPortal>
    </Dialog>
  );
}
