import { useEffect, useRef, useState, type CSSProperties, type ReactNode } from "react";
import { clampListWidth, LIST_MIN, LIST_STEP, PANE_MIN } from "../../lib/prefs/panelWidth";
import { usePrefs } from "../../lib/prefs/usePrefs";

/** `listw` is null until the reader drags, meaning the layout's own width. */
export default function SplitPane({
  hasChoice,
  list,
  pane,
}: {
  hasChoice: boolean;
  list: ReactNode;
  /** The right column, drawn as the `<aside>` it is: what the choice reads as. */
  pane: ReactNode;
}) {
  // Layout is measured on demand, not cached: a reset hands the width back to the
  // grid. `remeasure` only re-renders on window resize.
  const split = useRef<HTMLDivElement>(null);
  const column = useRef<HTMLDivElement>(null);
  const saved = usePrefs((s) => s.listWidth);
  const setListWidth = usePrefs((s) => s.setListWidth);
  // The width mid-drag, held locally so the store (and storage) is written once at the end.
  const [moving, setMoving] = useState<number | null>(null);
  const listw = moving ?? saved;
  // The drag's end listener closes over a stale render, so it reads the width from here.
  const latest = useRef<number | null>(listw);
  const [dragging, setDragging] = useState(false);
  const [, remeasure] = useState(0);
  // Width as of the last commit: a read during render sees the previous layout.
  const [shown, setShown] = useState(0);
  const grabbed = useRef<{ x: number; w: number } | null>(null);
  // Measure after every commit because other layout state can change the actual width.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => {
    setShown(drawn());
  });
  useEffect(() => {
    const again = () => remeasure((n) => n + 1);
    window.addEventListener("resize", again);
    return () => window.removeEventListener("resize", again);
  }, []);
  const room = () => split.current?.getBoundingClientRect().width ?? 0;
  /** The list column as it is drawn: the width the reader chose, or the grid's. */
  const drawn = () => column.current?.getBoundingClientRect().width ?? 0;
  // A saved width may be from a wider screen.
  const width = listw === null ? null : clampListWidth(listw, room());
  const bounds = () => ({ lo: LIST_MIN, hi: Math.max(LIST_MIN, room() - PANE_MIN) });
  /** Move the border, and put the width where the reader can watch it move. */
  const setWidth = (px: number) => {
    const { lo, hi } = bounds();
    const held = Math.round(Math.min(Math.max(px, lo), hi));
    latest.current = held;
    setMoving(held);
  };
  const keep = () => {
    setListWidth(latest.current);
    setMoving(null);
  };
  /** Move it from a single event — a key press — and keep it for the next visit. */
  const apply = (px: number) => {
    setWidth(px);
    keep();
  };
  const reset = () => {
    latest.current = null;
    keep();
  };
  // On the window, since the pointer leaves the 8px handle immediately.
  useEffect(() => {
    if (!dragging) return;
    const move = (ev: PointerEvent) => {
      // Move by pointer delta: the handle has its own column, so clientX - box.left
      // would offset the line from the pointer.
      const from = grabbed.current;
      if (from) setWidth(from.w + (ev.clientX - from.x));
    };
    const up = () => {
      grabbed.current = null;
      setDragging(false);
      keep();
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
    window.addEventListener("pointercancel", up);
    return () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
      window.removeEventListener("pointercancel", up);
    };
    // setWidth and keep read the DOM and refs; re-subscribe only on drag start/stop.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dragging]);

  const onKeyDown = (ev: React.KeyboardEvent) => {
    // From where the border is, not from where it was when the page loaded.
    const here = listw ?? drawn();
    const { lo, hi } = bounds();
    if (ev.key === "ArrowLeft") apply(Math.max(lo, here - LIST_STEP));
    else if (ev.key === "ArrowRight") apply(Math.min(hi, here + LIST_STEP));
    else if (ev.key === "Home") apply(lo);
    else if (ev.key === "End") apply(hi);
    else return;
    ev.preventDefault();
  };

  return (
    <div
      className={`ibsplit grid min-w-0 grid-cols-[minmax(0,1fr)] gap-4 min-[60rem]:min-h-0 min-[60rem]:grid-cols-[var(--listw,minmax(15rem,24rem))_.5rem_minmax(0,1fr)] min-[60rem]:items-stretch min-[60rem]:gap-x-0 min-[60rem]:flex-1${hasChoice ? " has-choice" : ""}`}
      ref={split}
      style={width === null ? undefined : ({ "--listw": `${width}px` } as CSSProperties)}
    >
      <div
        className={`flex min-w-0 flex-col gap-3 px-1 pt-3 min-[60rem]:h-full min-[60rem]:min-h-0 min-[60rem]:pr-0${hasChoice ? " max-[60rem]:hidden" : ""}`}
        ref={column}
      >
        {list}
      </div>

      {/* Arrow keys move it; double-click resets to the layout's width. */}
      <div
        className={`relative hidden cursor-col-resize touch-none self-stretch before:absolute before:inset-y-0 before:right-0 before:m-0 before:w-px before:bg-line before:content-[''] focus-visible:outline-none focus-visible:before:w-0.5 focus-visible:before:bg-accent min-[60rem]:block [&:hover]:before:w-0.5 [&:hover]:before:bg-accent${dragging ? " before:w-0.5 before:bg-accent" : ""}`}
        role="separator"
        aria-orientation="vertical"
        aria-label="Resize the list"
        aria-valuenow={Math.round(width ?? shown)}
        aria-valuemin={LIST_MIN}
        aria-valuemax={Math.round(bounds().hi)}
        title="Drag to resize the list — double-click to reset"
        tabIndex={0}
        onPointerDown={(ev) => {
          if (ev.button !== 0) return;
          ev.preventDefault();
          grabbed.current = { x: ev.clientX, w: drawn() };
          setDragging(true);
        }}
        onDoubleClick={reset}
        onKeyDown={onKeyDown}
      />

      {pane}
    </div>
  );
}
