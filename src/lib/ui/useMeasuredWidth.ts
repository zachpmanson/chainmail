import { useEffect, useState, type RefObject } from "react";

/** An element's drawn width, kept current as anything resizes it; 0 until first measured. */
export default function useMeasuredWidth(ref: RefObject<HTMLElement | null>) {
  const [width, setWidth] = useState(0);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const observer = new ResizeObserver(() => setWidth(el.getBoundingClientRect().width));
    observer.observe(el);
    return () => observer.disconnect();
  }, [ref]);
  return width;
}
