import { IconArrowBackUp } from "@tabler/icons-react";
import type { ReplyTarget } from "../../lib/timeline/replyTarget";

export default function ReplyLink({ parent }: { parent: ReplyTarget | null }) {
  if (!parent)
    return (
      <span className="tstart rounded-sm border border-line px-1 text-2xs font-bold tracking-[.09em] text-muted uppercase">
        thread start
      </span>
    );
  return (
    <a
      className="par inline-flex items-center gap-1 text-2xs text-muted no-underline hover:text-accent"
      href={`#${parent.anchor}`}
      /* In column mode CSS hides the label (.parlbl), so the title is the only full statement. */
      title={`In reply to ${parent.whoTitle ?? parent.who}, ${parent.when}`}
    >
      <IconArrowBackUp className="block size-3.5 shrink-0" aria-hidden="true" />
      <span className="parlbl">
        in reply to <b className="font-[650] text-inherit">{parent.who}</b>, {parent.when}
      </span>
    </a>
  );
}
