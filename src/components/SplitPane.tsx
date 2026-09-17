import { useEffect, useRef, useState, type CSSProperties, type ReactNode } from "react";
import {
  clampListWidth,
  LIST_MIN,
  LIST_STEP,
  PANE_MIN,
  readListWidth,
  rememberListWidth,
} from "../lib/panelWidth";

/**
 * The two-panel workspace: a list on the left, what is being read out of it on
 * the right, and the border between them as the control that says how much of
 * each. The inbox and the search page are the same shape — both are "pick
 * something out of a list and read it" — so the shape is one component rather
 * than two arrangements that drift.
 *
 * Neither column scrolls the page: the list scrolls itself and the pane scrolls
 * itself, which is why the split asks for the height it is given (`flex:1`) and
 * why the page that holds it wears a body class (see the workspace rules in
 * select.css). Below the width where both fit, the list steps aside entirely
 * once something is chosen — `hasChoice` — and the pane's own head carries the
 * way back.
 *
 * The width the reader drags is theirs and outlives the visit; `listw` is null
 * until they drag, which is the layout's own width for the screen rather than a
 * width anyone chose — a different thing, and the reason the splitter can be
 * reset to something rather than only moved away from it.
 */
export function SplitPane({
  hasChoice,
  list,
  pane,
}: {
  hasChoice: boolean;
  /** The left column: whatever the list is made of, plus anything that belongs
   *  at its head (the inbox's folders). */
  list: ReactNode;
  /** The right column, drawn as the `<aside>` it is: what the choice reads as. */
  pane: ReactNode;
}) {
  // What the layout is *now* is read where it is needed rather than kept: a
  // measurement taken at mount is wrong the moment a reset hands the width back
  // to the grid, and the arrow keys then start from a width the list no longer
  // has. jsdom cannot see that — it lays nothing out — and a real browser caught
  // it, one key press away: ArrowRight from the default moved the border to 256
  // instead of 400. `remeasure` is only what makes React ask again when the
  // window changes size.
  const split = useRef<HTMLDivElement>(null);
  const column = useRef<HTMLDivElement>(null);
  const [listw, setListw] = useState<number | null>(readListWidth);
  // The same value as state, for the drag's end to read: the listener that ends a
  // drag was subscribed when the drag started and closes over the width from that
  // render, which is the one thing it cannot be trusted with.
  const latest = useRef<number | null>(listw);
  const [dragging, setDragging] = useState(false);
  const [, remeasure] = useState(0);
  // What the list is drawn at, as of the last commit. A render happens while the
  // DOM still shows the previous layout, so a width read *during* a render is the
  // one that was just replaced — a reset to the grid's own width left the
  // separator announcing 630 while the list had gone back to 384.
  const [shown, setShown] = useState(0);
  // What the border was grabbed at, and what the list was drawn at then: a drag
  // reads both to move the line by the pointer's travel rather than to it.
  const grabbed = useRef<{ x: number; w: number } | null>(null);
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
  // A width the reader chose is held to what this window can afford: it was
  // dragged on some other screen, and the one they are in now may be narrower.
  const width = listw === null ? null : clampListWidth(listw, room());
  const bounds = () => ({ lo: LIST_MIN, hi: Math.max(LIST_MIN, room() - PANE_MIN) });
  /** Move the border, and put the width where the reader can watch it move. */
  const setWidth = (px: number) => {
    const { lo, hi } = bounds();
    const held = Math.round(Math.min(Math.max(px, lo), hi));
    latest.current = held;
    setListw(held);
  };
  /** Move it from a single event — a key press — and keep it for the next visit. */
  const apply = (px: number) => {
    setWidth(px);
    rememberListWidth(latest.current);
  };
  const reset = () => {
    latest.current = null;
    setListw(null);
    rememberListWidth(null);
  };
  // Dragging listens on the window rather than on the handle: the pointer leaves
  // an eight-pixel border immediately, and a drag that stopped tracking there
  // would be a drag the reader has to aim at.
  useEffect(() => {
    if (!dragging) return;
    const move = (ev: PointerEvent) => {
      // By how far the pointer has travelled, not by where it is. The border sits
      // in a column of its own and the pane begins at it, so the split's own edge
      // is *not* the panel's: setting the width to `clientX - box.left` would put
      // the line that column's width to the right of the pointer, where a delta
      // leaves it exactly under the pointer the reader is dragging from.
      const from = grabbed.current;
      if (from) setWidth(from.w + (ev.clientX - from.x));
    };
    const up = () => {
      grabbed.current = null;
      setDragging(false);
      // Once, at the end: a drag is a hundred moves, and the reader's browser has
      // no use for a hundred writes to say one width.
      rememberListWidth(latest.current);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
    window.addEventListener("pointercancel", up);
    return () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
      window.removeEventListener("pointercancel", up);
    };
    // setWidth and rememberListWidth read the DOM, so they are not dependencies:
    // this effect re-subscribes when a drag starts and stops, which is all it is
    // for.
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
      className={`ibsplit${hasChoice ? " has-choice" : ""}${dragging ? " dragging" : ""}`}
      ref={split}
      // The list's width where the reader has said, and the grid's own
      // minmax() where they have not. Setting it as a custom property rather
      // than a style on the column keeps the CSS the one place that decides
      // what the two columns are.
      style={width === null ? undefined : ({ "--listw": `${width}px` } as CSSProperties)}
    >
      <div className="ibcol" ref={column}>
        {list}
      </div>

      {/* The border between the panels is the control. A separator rather
          than a slider: what it changes is the border itself, and a reader who
          cannot drag it can still move it with the arrow keys. Double-click
          puts it back to the width the layout chose. */}
      <div
        className="ibdrag"
        role="separator"
        aria-orientation="vertical"
        aria-label="Resize the list"
        aria-valuenow={Math.round(width ?? shown)}
        aria-valuemin={LIST_MIN}
        aria-valuemax={Math.round(bounds().hi)}
        title="Drag to resize the list — double-click to reset"
        tabIndex={0}
        onPointerDown={(ev) => {
          // Left button only: the right button opens a menu, and the middle
          // one is a scroll.
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
