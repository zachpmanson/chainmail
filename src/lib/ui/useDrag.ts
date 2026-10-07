import { useEffect, useLayoutEffect, useRef, useState, type PointerEvent } from "react";

/**
 * A primary-button drag, reported as the pointer's distance from where it was grabbed.
 * Listens on the window, since the pointer soon leaves a thin handle.
 */
export default function useDrag({
  onMove,
  onEnd,
}: {
  onMove: (dx: number) => void;
  onEnd: (dx: number) => void;
}) {
  const [from, setFrom] = useState<number | null>(null);
  // The window listeners live for the whole drag, so they call the latest render's handlers.
  const handlers = useRef({ onMove, onEnd });
  useLayoutEffect(() => {
    handlers.current = { onMove, onEnd };
  });

  useEffect(() => {
    if (from === null) return;
    let dx = 0;
    const move = (ev: globalThis.PointerEvent) => {
      dx = ev.clientX - from;
      handlers.current.onMove(dx);
    };
    const up = () => {
      setFrom(null);
      handlers.current.onEnd(dx);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
    window.addEventListener("pointercancel", up);
    return () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
      window.removeEventListener("pointercancel", up);
    };
  }, [from]);

  const start = (ev: PointerEvent) => {
    if (ev.button !== 0) return;
    ev.preventDefault();
    setFrom(ev.clientX);
  };

  return { dragging: from !== null, start };
}
