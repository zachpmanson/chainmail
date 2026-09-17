import { useSyncExternalStore } from "react";

/**
 * The notifications a write leaves behind: what an action did, and why one did
 * not happen. They are read in one place, in one shape, wherever they were
 * raised — the selection bar acting on ticked threads, the reading pane acting
 * on the open one.
 *
 * This is a module store rather than context, because raising one is not a
 * render: it happens in a mutation's callback, in whichever component made the
 * write, and the box that draws it is the shell's. A provider would mean every
 * writer had to be inside it and every reader had to re-render when nothing it
 * draws had changed; a store with one subscription leaves both of those alone.
 *
 * A notification about work that is over is **not** a report that lingers: it
 * says what was done, and once it has been read it is only a claim about a
 * moment that has passed. A refusal is the other case — it is something to act
 * on, so it stays until the reader does something about it (see push, and
 * SAID_MS in MailVerbs for the clock the other one is on).
 */
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

/**
 * Take one down. Also the reader's own way out: a refusal stands until it is
 * dealt with, and clicking it away is dealing with it.
 */
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

/**
 * Say something. `ttl` is how long it stands, in milliseconds, or null to leave
 * it up until it is dismissed — which is what a refusal wants and what a
 * sentence about finished work must not have.
 *
 * The id is returned so a caller can take its own notification down when the
 * thing it was about is gone: a sentence about a thread the reader has left, or
 * a set of ticks they have cleared, is a claim about something no longer on
 * screen. Waiting for the clock in that case was the bug the pane and the bar
 * both had to work around (see their own histories).
 */
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

/** Everything down. The pane and the bar both use the id they were given, so
 *  this exists for the one caller that means "all of it": a test. */
export function clearToasts() {
  for (const at of timers.values()) clearTimeout(at);
  timers.clear();
  if (toasts.length === 0) return;
  toasts = [];
  emit();
}
