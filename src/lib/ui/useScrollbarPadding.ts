import { useLayoutEffect, useState, type RefObject } from "react";

/** Overlay scrollbars consume no layout width; padding supplies that missing space. */
export default function useScrollbarPadding(ref: RefObject<HTMLElement | null>) {
  const [needed, setNeeded] = useState(false);
  useLayoutEffect(() => {
    const element = ref.current;
    if (!element) return;
    const measure = () => {
      if (element.offsetWidth === 0) return;
      const style = getComputedStyle(element);
      const borders =
        (Number.parseFloat(style.borderLeftWidth) || 0) +
        (Number.parseFloat(style.borderRightWidth) || 0);
      // offsetWidth/clientWidth include padding, so our own padding cancels out.
      const gutter = element.offsetWidth - element.clientWidth - borders;
      setNeeded(gutter < 1);
    };
    measure();
    if (typeof ResizeObserver === "undefined") {
      window.addEventListener("resize", measure);
      return () => window.removeEventListener("resize", measure);
    }
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => observer.disconnect();
  }, [ref]);
  return needed;
}
