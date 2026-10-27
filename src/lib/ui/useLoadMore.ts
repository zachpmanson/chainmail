import { useEffect, type RefObject } from "react";

/** Prefetch within 600px of the list bottom, including inside an overflow panel. */
export default function useLoadMore(
  sentinel: RefObject<HTMLElement | null>,
  enabled: boolean,
  more: () => unknown,
  // Rearm after fast requests even if their loading render was batched away.
  pages: unknown,
  updatedAt: number, // An unchanged background refetch can consume the trigger too.
) {
  useEffect(() => {
    const marker = sentinel.current;
    if (!marker || !enabled) return;
    let observer: IntersectionObserver | undefined;
    let active = true;
    let requested = false;
    const observe = () => {
      observer?.disconnect();
      if (requested) return;
      // rootMargin cannot extend through a clipping ancestor. Desktop scrolls
      // the list panel; mobile scrolls the document instead.
      let root = marker.parentElement;
      while (root && !/^(auto|scroll)$/.test(getComputedStyle(root).overflowY)) {
        root = root.parentElement;
      }
      const io = new IntersectionObserver(
        (entries) => {
          if (!active || requested || observer !== io) return;
          if (!entries.some((e) => e.isIntersecting)) return;
          requested = true;
          io.disconnect();
          void more();
        },
        { root, rootMargin: "0px 0px 600px 0px" },
      );
      observer = io;
      io.observe(marker);
    };
    observe();
    window.addEventListener("resize", observe);
    return () => {
      active = false;
      observer?.disconnect();
      window.removeEventListener("resize", observe);
    };
  }, [sentinel, enabled, more, pages, updatedAt]);
}
