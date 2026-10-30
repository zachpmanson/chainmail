import { Dialog } from "@base-ui/react/dialog";
import type { ReactNode } from "react";
import { cn } from "../../lib/ui/cn";

export default function DialogShell({
  label,
  children,
  panelClassName = "",
  onClose,
  dismissOnBackdrop = true,
  closeDisabled = false,
}: {
  label: string;
  children: ReactNode;
  panelClassName?: string;
  onClose: () => void;
  dismissOnBackdrop?: boolean;
  closeDisabled?: boolean;
}) {
  return (
    <Dialog.Root
      open
      disablePointerDismissal={!dismissOnBackdrop || closeDisabled}
      onOpenChange={(open, details) => {
        if (!open) {
          if (closeDisabled) details.cancel();
          else onClose();
        }
      }}
    >
      <Dialog.Portal>
        <Dialog.Backdrop forceRender className="fixed inset-0 z-55 bg-black/45" />
        <Dialog.Viewport className="fixed inset-0 z-55 flex items-center justify-center">
          <Dialog.Popup
            aria-modal="true"
            aria-label={label}
            className={cn(
              "flex max-h-[70vh] max-w-[44rem] flex-col rounded-lg border border-line bg-card shadow-[0_8px_40px_rgba(0,0,0,.35)]",
              panelClassName,
            )}
          >
            {children}
          </Dialog.Popup>
        </Dialog.Viewport>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
