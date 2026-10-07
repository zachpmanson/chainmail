import { useSyncExternalStore } from "react";

/** A module store rather than context: toasts are raised from mutation callbacks, not renders. */
export type Toast = {
  id: number;
  text: string;
  /** "note" is work that is over; "fail" is something to act on. */
  kind: "note" | "fail";
};

let toasts: Toast[] = [];
let nextID = 1;
const listeners = new Set<() => void>();
const timers = new Map<number, ReturnType<typeof setTimeout>>();

const emit = () => {
  for (const l of listeners) l();
};

function subscribe(fn: () => void) {
  listeners.add(fn);
  return () => {
    listeners.delete(fn);
  };
}

const snapshot = () => toasts;

/** The notifications standing right now, oldest first. */
export function useToasts(): Toast[] {
  return useSyncExternalStore(subscribe, snapshot, snapshot);
}

export function dismissToast(id: number) {
  const at = timers.get(id);
  if (at !== undefined) {
    clearTimeout(at);
    timers.delete(id);
  }
  const next = toasts.filter((t) => t.id !== id);
  if (next.length === toasts.length) return;
  toasts = next;
  emit();
}

/** `ttl` in ms, or null to stay until dismissed. */
export function pushToast(text: string, kind: Toast["kind"] = "note", ttl: number | null = null) {
  const id = nextID++;
  toasts = [...toasts, { id, text, kind }];
  emit();
  if (ttl !== null) {
    timers.set(
      id,
      setTimeout(() => {
        timers.delete(id);
        dismissToast(id);
      }, ttl),
    );
  }
  return id;
}

export function clearToasts() {
  for (const at of timers.values()) clearTimeout(at);
  timers.clear();
  if (toasts.length === 0) return;
  toasts = [];
  emit();
}
