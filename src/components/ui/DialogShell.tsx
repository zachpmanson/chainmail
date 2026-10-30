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
  fullScreen = false,
  initialFocus,
}: {
  label: string;
  children: ReactNode;
  panelClassName?: string;
  onClose: () => void;
  dismissOnBackdrop?: boolean;
  closeDisabled?: boolean;
  fullScreen?: boolean;
  initialFocus?: Dialog.Popup.Props["initialFocus"];
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
        <Dialog.Viewport
          className={cn(
            "fixed inset-0 flex items-center justify-center",
            fullScreen ? "z-60" : "z-55",
          )}
        >
          <Dialog.Popup
            aria-modal="true"
            aria-label={label}
            initialFocus={initialFocus}
            className={cn(
              "flex flex-col",
              fullScreen
                ? "size-full bg-bg"
                : "max-h-[70vh] max-w-[44rem] rounded-lg border border-line bg-card shadow-[0_8px_40px_rgba(0,0,0,.35)]",
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
