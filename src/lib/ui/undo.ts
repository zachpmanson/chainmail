export type UndoEntry = { label: string; undo: () => Promise<void> | void };

/** Forward writes and reversals share one FIFO; a failed reversal stays on top. */
export class UndoStack {
  private entries: UndoEntry[] = [];
  private tail: Promise<unknown> = Promise.resolve();
  private listeners = new Set<() => void>();
  private pending = 0;
  private snapshot = { label: "", pending: false };

  subscribe = (listener: () => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };
  getSnapshot = () => this.snapshot;
  private changed() {
    this.snapshot = { label: this.entries.at(-1)?.label ?? "", pending: this.pending > 0 };
    this.listeners.forEach((listener) => listener());
  }
  private enqueue<T>(task: () => Promise<T> | T): Promise<T> {
    this.pending++;
    this.changed();
    const result = this.tail.then(task);
    this.tail = result.catch(() => undefined);
    return result.finally(() => {
      this.pending--;
      this.changed();
    });
  }
  perform<T>(action: () => Promise<T> | T): Promise<T> {
    return this.enqueue(action);
  }
  // Called within perform, before it releases the queue, never on network completion outside it.
  remember(entry: UndoEntry) {
    this.entries.push(entry);
    if (this.entries.length > 100) this.entries.shift();
    this.changed();
  }
  undo(): Promise<string | undefined> {
    return this.enqueue(async () => {
      const entry = this.entries.at(-1);
      if (!entry) return undefined;
      await entry.undo();
      this.entries.pop();
      this.changed();
      return entry.label;
    });
  }
}

export const appUndo = new UndoStack();

type Key = Pick<
  KeyboardEvent,
  | "key"
  | "ctrlKey"
  | "metaKey"
  | "altKey"
  | "shiftKey"
  | "repeat"
  | "isComposing"
  | "defaultPrevented"
>;

export function isUndoKey(event: Key, path: readonly EventTarget[]): boolean {
  if (
    event.defaultPrevented ||
    event.repeat ||
    event.isComposing ||
    event.altKey ||
    event.shiftKey ||
    event.ctrlKey === event.metaKey ||
    event.key.toLowerCase() !== "z"
  )
    return false;
  // composedPath includes editable ancestors, even across a shadow root.
  return !path.some((target) => {
    const el = target as HTMLElement;
    return (
      typeof el.matches === "function" &&
      (el.matches("input, textarea, select") ||
        el.isContentEditable ||
        (el.hasAttribute("contenteditable") && el.getAttribute("contenteditable") !== "false") ||
        el.getAttribute("role") === "textbox" ||
        el.ownerDocument?.designMode === "on")
    );
  });
}
