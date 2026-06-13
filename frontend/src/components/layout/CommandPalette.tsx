import { CornerDownLeft, Search } from "lucide-react";
import { useState } from "react";

import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandShortcut,
} from "../ui/command";
import { NAV_ITEMS, type Navigate } from "./nav";

// CommandPalette is the ⌘K launcher: jump between sections, or run a ComicVine search for
// whatever's typed (seeded straight into Discover). cmdk handles fuzzy filtering; the
// ComicVine item carries the query in its value so it stays matchable as you type.
export function CommandPalette({
  open,
  onOpenChange,
  onNavigate,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onNavigate: Navigate;
}) {
  const [value, setValue] = useState("");
  const query = value.trim();

  function go(fn: () => void) {
    onOpenChange(false);
    setValue("");
    fn();
  }

  return (
    <CommandDialog open={open} onOpenChange={onOpenChange}>
      <CommandInput
        placeholder="Jump to a section, or search ComicVine…"
        value={value}
        onValueChange={setValue}
      />
      <CommandList>
        <CommandEmpty>No results.</CommandEmpty>

        {query ? (
          <CommandGroup heading="ComicVine">
            <CommandItem
              value={`comicvine search ${query}`}
              onSelect={() => go(() => onNavigate("search", { query }))}
            >
              <Search />
              <span>
                Search ComicVine for <span className="font-semibold text-foreground">{query}</span>
              </span>
              <CommandShortcut>
                <CornerDownLeft className="size-3" />
              </CommandShortcut>
            </CommandItem>
          </CommandGroup>
        ) : null}

        <CommandGroup heading="Go to">
          {NAV_ITEMS.map((item) => {
            const Icon = item.icon;
            return (
              <CommandItem
                key={item.route}
                value={`${item.label} ${item.hint}`}
                onSelect={() => go(() => onNavigate(item.route))}
              >
                <Icon />
                <span>{item.label}</span>
                <span className="ml-auto text-xs text-muted-foreground">{item.hint}</span>
              </CommandItem>
            );
          })}
        </CommandGroup>
      </CommandList>
    </CommandDialog>
  );
}
