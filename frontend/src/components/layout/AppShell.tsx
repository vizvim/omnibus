import type { ReactNode } from "react";

import { TooltipProvider } from "../ui/tooltip";
import type { Navigate, Route } from "./nav";
import { Sidebar } from "./Sidebar";
import { Topbar } from "./Topbar";

// AppShell is the persistent chrome: a left nav rail + a sticky topbar wrapping the routed
// page content. The page is keyed by the caller so each navigation re-runs the entry
// animation.
export function AppShell({
  active,
  onNavigate,
  onOpenCommand,
  children,
}: {
  active: Route;
  onNavigate: Navigate;
  onOpenCommand: () => void;
  children: ReactNode;
}) {
  return (
    <TooltipProvider delayDuration={200}>
      <div className="flex min-h-screen w-full">
        {/* Persistent rail at lg+; below lg it collapses and the Topbar hamburger drawer
            (MobileNavDrawer) becomes the nav. */}
        <Sidebar active={active} onNavigate={onNavigate} onOpenCommand={onOpenCommand} />
        <div className="flex min-w-0 flex-1 flex-col">
          <Topbar active={active} onNavigate={onNavigate} onOpenCommand={onOpenCommand} />
          <main className="mx-auto w-full max-w-6xl flex-1 px-5 py-8 md:px-8 md:py-10">
            {children}
          </main>
        </div>
      </div>
    </TooltipProvider>
  );
}
