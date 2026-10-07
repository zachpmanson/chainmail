import { useEffect, type RefObject } from "react";

/** Calls `more` when the sentinel comes within 300px of view, while `enabled`. */
export default function useLoadMore(
  sentinel: RefObject<HTMLElement | null>,
  enabled: boolean,
  more: () => unknown,
) {
  useEffect(() => {
    const marker = sentinel.current;
    if (!marker || !enabled) return;
    const io = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting)) void more();
      },
      { rootMargin: "300px" },
    );
    io.observe(marker);
    return () => io.disconnect();
  }, [sentinel, enabled, more]);
}
