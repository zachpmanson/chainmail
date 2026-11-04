import { useRef, type ReactNode } from "react";
import { cn } from "../../lib/ui/cn";
import useScrollbarPadding from "../../lib/ui/useScrollbarPadding";

/** Keep equivalent list spacing with native classic or overlay scrollbars. */
export default function ThreadListScroll({ children }: { children: ReactNode }) {
  const scroller = useRef<HTMLDivElement>(null);
  const needsPadding = useScrollbarPadding(scroller);
  return (
    <div
      ref={scroller}
      data-thread-list-scroll
      className={cn(
        "min-w-0 supports-[not_selector(::-webkit-scrollbar)]:[scrollbar-color:var(--line)_transparent] min-[60rem]:min-h-0 min-[60rem]:flex-1 min-[60rem]:overflow-y-scroll min-[60rem]:rounded-lg min-[60rem]:border min-[60rem]:border-line min-[60rem]:bg-card [&::-webkit-scrollbar]:w-2.5 [&::-webkit-scrollbar-thumb]:rounded-full [&::-webkit-scrollbar-thumb]:border-2 [&::-webkit-scrollbar-thumb]:border-card [&::-webkit-scrollbar-thumb]:bg-line [&::-webkit-scrollbar-thumb:hover]:bg-muted [&::-webkit-scrollbar-track]:bg-transparent",
        needsPadding && "min-[60rem]:pe-2.5",
      )}
    >
      {children}
    </div>
  );
}
