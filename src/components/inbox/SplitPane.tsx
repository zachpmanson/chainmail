import { useRef, useState, type CSSProperties, type ReactNode } from "react";
import { clampListWidth, LIST_MIN, LIST_STEP } from "../../lib/prefs/panelWidth";
import { usePrefs } from "../../lib/prefs/usePrefs";
import useDrag from "../../lib/ui/useDrag";
import useMeasuredWidth from "../../lib/ui/useMeasuredWidth";

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
  const split = useRef<HTMLDivElement>(null);
  const column = useRef<HTMLDivElement>(null);
  const room = useMeasuredWidth(split);
  /** The list column as it is drawn: the width the reader chose, or the grid's. */
  const drawn = useMeasuredWidth(column);
  const saved = usePrefs((s) => s.listWidth);
  const setListWidth = usePrefs((s) => s.setListWidth);
  // The width mid-drag, held locally so the store (and storage) is written once at the end.
  const [moving, setMoving] = useState<number | null>(null);
  const listw = moving ?? saved;
  // A saved width may be from a wider screen.
  const width = listw === null ? null : clampListWidth(listw, room);
  const widest = clampListWidth(Infinity, room);

  const grabbedAt = useRef(0);
  const drag = useDrag({
    onMove: (dx) => setMoving(clampListWidth(grabbedAt.current + dx, room)),
    onEnd: (dx) => {
      setListWidth(clampListWidth(grabbedAt.current + dx, room));
      setMoving(null);
    },
  });

  const onKeyDown = (ev: React.KeyboardEvent) => {
    // From where the border is, not from where it was when the page loaded.
    const here = width ?? drawn;
    const next: number | undefined = {
      ArrowLeft: here - LIST_STEP,
      ArrowRight: here + LIST_STEP,
      Home: LIST_MIN,
      End: widest,
    }[ev.key];
    if (next === undefined) return;
    ev.preventDefault();
    setListWidth(clampListWidth(next, room));
  };

  return (
    <div
      className={`ibsplit grid min-w-0 grid-cols-[minmax(0,1fr)] gap-4 min-[60rem]:min-h-0 min-[60rem]:grid-cols-[var(--listw,minmax(15rem,24rem))_.5rem_minmax(0,1fr)] min-[60rem]:items-stretch min-[60rem]:gap-x-0 min-[60rem]:flex-1${hasChoice ? " has-choice" : ""}`}
      ref={split}
      style={width === null ? undefined : ({ "--listw": `${width}px` } as CSSProperties)}
    >
      <div
        className={`flex min-w-0 flex-col gap-3 px-2 pt-3 min-[60rem]:h-full min-[60rem]:min-h-0 min-[60rem]:pr-0${hasChoice ? " max-[60rem]:hidden" : ""}`}
        ref={column}
      >
        {list}
      </div>

      {/* Arrow keys move it; double-click resets to the layout's width. */}
      <div
        className={`relative hidden cursor-col-resize touch-none self-stretch before:absolute before:inset-y-0 before:right-0 before:m-0 before:w-px before:bg-line before:content-[''] focus-visible:outline-none focus-visible:before:w-0.5 focus-visible:before:bg-accent min-[60rem]:block [&:hover]:before:w-0.5 [&:hover]:before:bg-accent${drag.dragging ? " before:w-0.5 before:bg-accent" : ""}`}
        role="separator"
        aria-orientation="vertical"
        aria-label="Resize the list"
        aria-valuenow={Math.round(width ?? drawn)}
        aria-valuemin={LIST_MIN}
        aria-valuemax={widest}
        title="Drag to resize the list — double-click to reset"
        tabIndex={0}
        onPointerDown={(ev) => {
          grabbedAt.current = drawn;
          drag.start(ev);
        }}
        onDoubleClick={() => setListWidth(null)}
        onKeyDown={onKeyDown}
      />

      {pane}
    </div>
  );
}
