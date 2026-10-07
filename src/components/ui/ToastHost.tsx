import { dismissToast, useToasts } from "../../lib/ui/toasts";
import { Button } from "./controls";

/** Mounted once in the shell so a toast outlives the page that raised it. */
export default function ToastHost() {
  const toasts = useToasts();
  if (toasts.length === 0) return null;
  return (
    <div
      className="fixed right-[.9rem] bottom-[.9rem] z-[80] flex max-w-[min(26rem,calc(100vw-1.8rem))] flex-col items-end gap-1.5 pointer-events-none"
      role="status"
      aria-live="polite"
    >
      {toasts.map((t) => (
        <div
          key={t.id}
          className={`toast flex items-start gap-2 pointer-events-auto rounded-[7px] border border-line border-l-[3px] bg-card px-2 py-2 pl-3 text-[.78rem] leading-[1.35] text-fg shadow-[0_6px_20px_rgba(0,0,0,.22)]${t.kind === "fail" ? " border-red-700" : ""}`}
        >
          <span className="flex-1">{t.text}</span>
          <Button
            type="button"
            variant="quiet"
            className="min-h-0 size-5 shrink-0 border-0 px-0.5 py-0 leading-none text-muted hover:text-accent"
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
