import Checkbox from "../ui/Checkbox";
import { type ReactNode } from "react";
import { type ChainHit } from "../../lib/api/api";
import { useReadAction } from "../../lib/inbox/mailActions";

/** Same floor refresh holds semantic-only proposals to (internal/refresh). */
export const HIGHLIGHT_FLOOR = 0.8;

export const subjectOf = (thread: { subject?: string }) => thread.subject || "(no subject)";

/** Shared frame for FullThreadRow and CompactThreadRow. */
export default function ThreadRow({
  thread,
  checked,
  current,
  className,
  openClassName,
  aside,
  children,
  onToggle,
  onOpen,
}: {
  thread: ChainHit;
  checked: boolean;
  /** Whether this is the thread the pane is reading. */
  current: boolean;
  /** The variant's own classes on the row. */
  className?: string;
  /** The variant's own classes on the row body, a flex box that lays out its children. */
  openClassName?: string;
  /** What the variant draws in the row beside the body, outside its hit area. */
  aside?: ReactNode;
  /** The body of the row: what the variant says about the thread. */
  children: ReactNode;
  onToggle: () => void;
  onOpen: () => void;
}) {
  const subject = subjectOf(thread);
  // Double-click toggles read optimistically (see lib/inbox/lists) and rolls back on refusal.
  // No toggle when the caller doesn't know the unread count: there's no state to invert.
  const toggle = useReadAction();
  const flip = thread.unread === undefined ? null : thread.unread === 0;
  // Opening is idempotent, so the double-click's first click has nothing to undo.
  const press = () => {
    // Guarding here stops a second click pushing a duplicate history entry.
    if (!current) onOpen();
  };
  return (
    <li
      className={[
        "group relative hover:bg-[color-mix(in_srgb,var(--muted)_22%,var(--card))]",
        (thread.unread === undefined || thread.unread <= 0) &&
          "bg-[color-mix(in_srgb,var(--muted)_12%,var(--card))]",
        current && "[box-shadow:inset_2px_0_0_var(--accent)]",
        className,
      ]
        .filter(Boolean)
        .join(" ")}
      // Deep links (`?open=`) find the row to scroll to by this attribute.
      data-root={thread.rootExtId}
    >
      <button
        type="button"
        className={[
          "flex w-full cursor-pointer appearance-none border-0 bg-transparent py-2 pr-1.5 pl-3 text-left font-[inherit] text-inherit shadow-none",
          openClassName,
        ]
          .filter(Boolean)
          .join(" ")}
        onClick={press}
        onDoubleClick={
          flip === null || toggle.isPending
            ? undefined
            : () => toggle.mutate({ body: { chain: thread.rootExtId, unread: flip } })
        }
        aria-label={subject}
        aria-current={current ? "true" : undefined}
        title={
          thread.unread > 0
            ? `${thread.unread} unread message${thread.unread === 1 ? "" : "s"} in this thread`
            : undefined
        }
      >
        {children}
      </button>
      {aside}
      <label
        className="absolute top-0 right-0 flex w-6 cursor-pointer justify-end pt-2 pr-1.5 pb-1.5"
        title="include this thread in a page"
      >
        <Checkbox
          checked={checked}
          onChange={onToggle}
          aria-label={`Select ${subject}`}
          className="m-0 cursor-pointer"
        />
      </label>
    </li>
  );
}
