import { Plus, Search } from "lucide-react";

import { Button } from "../ui/button";
import type { Navigate } from "./nav";

export function Topbar({
  onNavigate,
  onOpenCommand,
}: {
  onNavigate: Navigate;
  onOpenCommand: () => void;
}) {
  return (
    <header className="sticky top-0 z-30 flex h-16 items-center gap-3 border-b border-border bg-background/70 px-5 backdrop-blur-xl md:px-8">
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
        <div className="mr-1 hidden items-center gap-2 sm:flex">
          <span className="size-2 rounded-full bg-tn-green shadow-[0_0_8px_0_rgba(158,206,106,0.8)]" />
          <span className="font-mono text-xs text-muted-foreground">online</span>
        </div>
        <Button onClick={() => onNavigate("search")} className="gap-1.5">
          <Plus className="size-4" />
          <span className="hidden sm:inline">Add series</span>
        </Button>
      </div>
    </header>
  );
}
