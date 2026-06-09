import * as Dialog from "@radix-ui/react-dialog";
import type { ReactNode } from "react";

// AddConfirmDialog wraps the Radix Dialog primitive (a11y focus management) to confirm
// a mutation before firing it. It defaults to the "Add to Library" copy used by the
// Search flow, but title/description/confirm label and the confirm button tint are
// overridable so the same pattern serves destructive confirms (e.g. delete indexer,
// UI-SPEC §destructive).
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
  const confirmTint = destructive ? "bg-red-600" : "bg-blue-600";
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Trigger asChild>{trigger}</Dialog.Trigger>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 bg-black/40" />
        <Dialog.Content className="fixed left-1/2 top-1/2 w-80 -translate-x-1/2 -translate-y-1/2 rounded bg-white p-6">
          <Dialog.Title className="text-xl font-semibold">{title ?? "Add to Library"}</Dialog.Title>
          <Dialog.Description className="mt-2 text-base text-slate-600">
            {description ?? <>Add “{seriesName}” to your library and start tracking its issues?</>}
          </Dialog.Description>
          <div className="mt-4 flex justify-end gap-2">
            <Dialog.Close asChild>
              <button type="button" className="rounded bg-slate-100 px-4 py-2 text-sm font-semibold">
                Cancel
              </button>
            </Dialog.Close>
            <button
              type="button"
              disabled={pending}
              onClick={onConfirm}
              className={`rounded ${confirmTint} px-4 py-2 text-sm font-semibold text-white disabled:opacity-50`}
            >
              {confirmLabel}
            </button>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
