import type { ReactNode } from "react";

/**
 * The list column's scroller. macOS overlay scrollbars hide until scrolled; a styled
 * WebKit scrollbar is always drawn. Chrome ignores those pseudo-elements once
 * scrollbar-color is set, so only Firefox gets that.
 */
export default function ThreadListScroll({ children }: { children: ReactNode }) {
  return (
    <div className="min-w-0 supports-[not_selector(::-webkit-scrollbar)]:[scrollbar-color:var(--line)_transparent] min-[60rem]:min-h-0 min-[60rem]:flex-1 min-[60rem]:overflow-y-scroll min-[60rem]:rounded-lg min-[60rem]:border min-[60rem]:border-line min-[60rem]:bg-card [&::-webkit-scrollbar]:w-2.5 [&::-webkit-scrollbar-thumb]:rounded-full [&::-webkit-scrollbar-thumb]:border-2 [&::-webkit-scrollbar-thumb]:border-card [&::-webkit-scrollbar-thumb]:bg-line [&::-webkit-scrollbar-thumb:hover]:bg-muted [&::-webkit-scrollbar-track]:bg-transparent">
      {children}
    </div>
  );
}
