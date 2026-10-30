import { Toast } from "@base-ui/react/toast";
import { useEffect, useRef } from "react";
import { dismissToast, useToasts } from "../../lib/ui/toasts";
import { buttonClasses } from "./styles";

function ToastList() {
  const notifications = useToasts();
  const { toasts, add, close } = Toast.useToastManager();
  const mirrored = useRef(new Set<number>());

  // Keep the module store's lifetime (including time spent without a mounted host).
  // The context manager avoids the external manager's passive subscription startup gap.
  useEffect(() => {
    const ids = new Set(notifications.map((notification) => notification.id));
    for (const notification of notifications) {
      if (mirrored.current.has(notification.id)) continue;
      mirrored.current.add(notification.id);
      add({
        id: String(notification.id),
        title: notification.text,
        type: notification.kind,
        priority: "low",
        timeout: 0,
        onClose: () => dismissToast(notification.id),
      });
    }
    for (const id of mirrored.current) {
      if (ids.has(id)) continue;
      mirrored.current.delete(id);
      close(String(id));
    }
  }, [notifications, add, close]);

  return (
    <Toast.Portal>
      <Toast.Viewport
        onKeyDown={(event) => {
          // Base closes the focused toast; the same Escape must not reach inbox/dialog handlers.
          if (event.key === "Escape") event.stopPropagation();
        }}
        className="pointer-events-none fixed right-[.9rem] bottom-[.9rem] z-80 flex max-w-[min(26rem,calc(100vw-1.8rem))] flex-col items-end gap-1.5 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
      >
        {[...toasts].reverse().map((toast) => (
          <Toast.Root
            key={toast.id}
            toast={toast}
            swipeDirection={[]}
            className={`pointer-events-auto flex items-start gap-2 rounded-md border border-l-3 border-line bg-card p-2 pl-3 text-xs/snug text-fg shadow-[0_6px_20px_rgba(0,0,0,.22)] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent min-[60rem]:motion-safe:animate-toastin${toast.type === "fail" ? " border-red-700" : ""}`}
          >
            <Toast.Title render={<span />} className="flex-1" />
            <Toast.Close
              className={buttonClasses(
                "bare",
                "normal",
                "inline-flex size-5 shrink-0 items-center justify-center rounded-md px-0.5 text-sm leading-none font-semibold text-muted transition-colors hover:text-accent",
              )}
              aria-label={`Dismiss: ${toast.title}`}
              aria-hidden={false}
              title="Dismiss"
            >
              ×
            </Toast.Close>
          </Toast.Root>
        ))}
      </Toast.Viewport>
    </Toast.Portal>
  );
}

/** Mounted once in the shell so a toast outlives the page that raised it. */
export default function ToastHost() {
  return (
    <Toast.Provider timeout={0} limit={Infinity}>
      <ToastList />
    </Toast.Provider>
  );
}
