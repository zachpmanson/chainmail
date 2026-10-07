import { dismissToast, useToasts } from "../../lib/ui/toasts";
import Button from "./Button";

/** Mounted once in the shell so a toast outlives the page that raised it. */
export default function ToastHost() {
  const toasts = useToasts();
  if (toasts.length === 0) return null;
  return (
    <div
      className="pointer-events-none fixed right-[.9rem] bottom-[.9rem] z-80 flex max-w-[min(26rem,calc(100vw-1.8rem))] flex-col items-end gap-1.5"
      role="status"
      aria-live="polite"
    >
      {toasts.map((t) => (
        <div
          key={t.id}
          className={`pointer-events-auto flex items-start gap-2 rounded-md border border-l-3 border-line bg-card p-2 pl-3 text-xs/snug text-fg shadow-[0_6px_20px_rgba(0,0,0,.22)] min-[60rem]:motion-safe:animate-toastin${t.kind === "fail" ? " border-red-700" : ""}`}
        >
          <span className="flex-1">{t.text}</span>
          <Button
            type="button"
            variant="bare"
            className="inline-flex size-5 shrink-0 items-center justify-center rounded-md px-0.5 text-sm leading-none font-semibold text-muted transition-colors hover:text-accent"
            onClick={() => dismissToast(t.id)}
            aria-label={`Dismiss: ${t.text}`}
            title="Dismiss"
          >
            ×
          </Button>
        </div>
      ))}
    </div>
  );
}
