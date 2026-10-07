import Source from "../thread/Source";
import type { Row, View } from "../../lib/timeline/derive";
import Message from "../thread/Message";
import ReplyLink from "../thread/ReplyLink";
import Edits from "./Edits";
import { trimBody } from "../../lib/message/trimBody";
import { replyTarget } from "../../lib/timeline/replyTarget";

const html = (s: string) => ({ __html: s });

/** Adapts a spec row to `Message`; system notes have no sender or bubble so are drawn here. */
export default function EntryBlock({
  row,
  v,
  mark,
  anchorByGmail,
  onPull,
  pulling,
  mediaBase,
}: {
  row: Row;
  v: View;
  mark?: "new" | "revised";
  anchorByGmail: Map<string, string>;
  onPull?: (extId: string) => void;
  pulling?: string | null;
  mediaBase?: string;
}) {
  const e = row.entry;
  const grid = { gridColumn: row.lane + 1, gridRow: row.row };

  if (e.kind === "note") {
    return (
      <div
        className={`sys scroll-mt-6 mx-auto my-3 max-w-[44rem] border border-dashed border-line rounded-lg bg-quote px-4 py-2 text-center${row.isChainStart ? " chstart" : ""}${mark === "new" ? " [border-left:3px_solid_var(--o1)]" : ""}`}
        id={row.id}
        data-ch={row.lane}
        style={grid}
      >
        <div className="mb-0.5 text-2xs tabular-nums text-muted">
          <a
            className="rounded-sm text-inherit underline-offset-2 decoration-accent no-underline hover:text-accent hover:underline hover:decoration-dotted focus-visible:outline-[1.5px] focus-visible:outline-accent focus-visible:outline-offset-1"
            href={`#${row.id}`}
            title="Link to this note"
          >
            {e.date}
          </a>
        </div>
        <div className="mb-1 text-xs font-bold uppercase tracking-[.08em] text-muted">
          {e.label}
        </div>
        <div className="bd" dangerouslySetInnerHTML={html(trimBody(e.body))} />
        <ReplyLink parent={replyTarget(row, v)} />
      </div>
    );
  }

  return (
    <Message
      id={row.id}
      body={e.body}
      sender={e.sender}
      senderTitle={v.whoTitle(e.sender ?? "")}
      org={e.org}
      orgSlot={row.orgSlot}
      avatarClass={row.avatarClass}
      me={e.me}
      quoted={e.quoted}
      mentions={e.mentions}
      attachments={e.attachments}
      extId={e.extId}
      onPull={onPull}
      pulling={pulling}
      mediaBase={mediaBase}
      to={e.to}
      toTitle={v.whoTitle}
      subject={e.subject}
      stamp={row.stamp}
      style={grid}
      lane={row.lane}
      chainStart={row.isChainStart}
      mark={mark}
      reply={<ReplyLink parent={replyTarget(row, v)} />}
      edits={<Edits edits={row.edits} fallbackWho={v.title} />}
      source={<Source source={e.source} anchorByGmail={anchorByGmail} />}
      copyJson={{
        id: row.id,
        thread: row.chain ?? null,
        entry: row.entry,
        edits: row.edits?.length ? row.edits : undefined,
      }}
    />
  );
}
