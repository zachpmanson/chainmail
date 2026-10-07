export interface ReplyTarget {
  /** a spec row id on a page, `entry-N` in the pane */
  anchor: string;
  /** a name, or a system note's label */
  who?: string;
  /** "Name <address>" (see lib/message/who); absent falls back to `who` */
  whoTitle?: string;
  /** Their clock as the parent bubble states it, e.g. "Mon 2 Mar 2026 09:15". */
  when?: string;
}

export default function ReplyLink({ parent }: { parent: ReplyTarget | null }) {
  if (!parent)
    return (
      <span className="tstart rounded-[4px] border border-line px-1 text-[.6rem] font-bold uppercase tracking-[.09em] text-muted">
        thread start
      </span>
    );
  return (
    <a
      className="par inline-flex items-center gap-1 text-[.66rem] text-muted no-underline hover:text-accent"
      href={`#${parent.anchor}`}
      /* In column mode CSS hides the label (.parlbl), so the title is the only full statement. */
      title={`In reply to ${parent.whoTitle ?? parent.who}, ${parent.when}`}
    >
      <span className="block translate-y-[.045em] text-[.8rem] leading-none">&#8617;</span>
      <span className="parlbl">
        in reply to <b className="font-[650] text-inherit">{parent.who}</b>, {parent.when}
      </span>
    </a>
  );
}
