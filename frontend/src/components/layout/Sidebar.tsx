import { Command as CommandIcon } from "lucide-react";

import { cn } from "../../lib/utils";
import { Tooltip, TooltipContent, TooltipTrigger } from "../ui/tooltip";
import { NAV_ITEMS, type Navigate, type Route } from "./nav";

// LogoMark is three offset comic spines — a tiny nod to a shelf of books, tinted with the
// Tokyo Night accent trio (blue / magenta / cyan).
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

function NavButton({
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
    <Tooltip delayDuration={300}>
      <TooltipTrigger asChild>
        <button
          type="button"
          onClick={onClick}
          aria-current={active ? "page" : undefined}
          className={cn(
            "group relative flex h-10 w-full items-center gap-3 rounded-lg px-3 text-sm font-medium transition-all duration-150",
            active
              ? "bg-secondary text-foreground"
              : "text-muted-foreground hover:bg-secondary/60 hover:text-foreground",
          )}
        >
          {/* active accent rail */}
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
          <span className="hidden truncate lg:inline">{item.label}</span>
        </button>
      </TooltipTrigger>
      <TooltipContent side="right" className="lg:hidden">
        {item.label}
        <span className="ml-1.5 text-muted-foreground">{item.hint}</span>
      </TooltipContent>
    </Tooltip>
  );
}

export function Sidebar({
  active,
  onNavigate,
  onOpenCommand,
}: {
  active: Route;
  onNavigate: Navigate;
  onOpenCommand: () => void;
}) {
  return (
    <aside className="sticky top-0 flex h-screen w-[68px] shrink-0 flex-col border-r border-border bg-card/40 px-3 py-4 backdrop-blur-xl lg:w-64">
      {/* brand */}
      <button
        type="button"
        onClick={() => onNavigate("library")}
        className="mb-7 flex h-10 items-center gap-2.5 rounded-lg px-1.5 transition-colors hover:bg-secondary/40"
      >
        <LogoMark className="size-7 shrink-0" />
        <span className="hidden items-baseline font-display text-xl font-semibold tracking-tight text-foreground lg:flex">
          omnibus
          <span className="ml-0.5 inline-block h-4 w-[7px] translate-y-px animate-pulse rounded-[1px] bg-primary" />
        </span>
      </button>

      {/* primary nav */}
      <nav className="flex flex-col gap-1">
        {NAV_ITEMS.map((item) => (
          <NavButton
            key={item.route}
            item={item}
            active={active === item.route}
            onClick={() => onNavigate(item.route)}
          />
        ))}
      </nav>

      <div className="mt-auto flex flex-col gap-3">
        <button
          type="button"
          onClick={onOpenCommand}
          className="group flex h-10 items-center gap-3 rounded-lg border border-border bg-tn-night/40 px-3 text-sm text-muted-foreground transition-colors hover:border-border-strong hover:text-foreground"
        >
          <CommandIcon className="size-[18px] shrink-0" />
          <span className="hidden flex-1 text-left lg:inline">Command</span>
          <kbd className="hidden rounded border border-border-strong/70 bg-secondary px-1.5 font-mono text-[0.65rem] text-muted-foreground lg:inline">
            ⌘K
          </kbd>
        </button>
        <div className="hidden px-1.5 font-mono text-[0.65rem] uppercase tracking-wider text-muted-foreground/60 lg:block">
          omnibus · v0.1
        </div>
      </div>
    </aside>
  );
}
