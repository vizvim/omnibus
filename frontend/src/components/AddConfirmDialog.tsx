import type { ReactNode } from "react";

import { Button } from "./ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "./ui/dialog";

// AddConfirmDialog confirms a mutation before firing it. It defaults to the "Add to
// Library" copy used by the Search flow, but title/description/confirm label and the
// confirm tint are overridable so the same pattern serves destructive confirms (e.g.
// delete indexer).
export function AddConfirmDialog({
  trigger,
  seriesName,
  onConfirm,
  open,
  onOpenChange,
  pending,
  title,
  description,
  confirmLabel = "Add to Library",
  destructive = false,
}: {
  trigger: ReactNode;
  seriesName: string;
  onConfirm: () => void;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  pending: boolean;
  title?: string;
  description?: ReactNode;
  confirmLabel?: string;
  destructive?: boolean;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogTrigger asChild>{trigger}</DialogTrigger>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle>{title ?? "Add to Library"}</DialogTitle>
          <DialogDescription>
            {description ?? (
              <>Add “{seriesName}” to your library and start tracking its issues?</>
            )}
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <DialogClose asChild>
            <Button variant="ghost">Cancel</Button>
          </DialogClose>
          <Button
            variant={destructive ? "destructive" : "default"}
            disabled={pending}
            onClick={onConfirm}
          >
            {confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
