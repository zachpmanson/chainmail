import Source from "../thread/Source";
import { derive, type Row, type View } from "../../lib/timeline/derive";
import type { Timeline as Spec } from "../../lib/timeline/spec";
import DiffPanel from "./DiffPanel";
import Legend from "./Legend";
import SourcesPanel from "./SourcesPanel";
import { type ThreadFilter } from "./Panels";
import ParticipantsPanel from "../thread/ParticipantsPanel";
import Minimap from "./Minimap";
import Message from "../thread/Message";
import ReplyLink from "../thread/ReplyLink";
import { type ReplyTarget } from "../thread/ReplyLink";
import Edits from "./Edits";
import { trimBody } from "../../lib/message/trimBody";

const html = (s: string) => ({ __html: s });

const toolbarButtonClasses =
  "inline-flex items-center rounded-md border border-line bg-card px-2 py-1 text-[.66rem] font-bold uppercase tracking-[.08em] text-muted hover:border-accent hover:text-accent aria-pressed:border-accent aria-pressed:bg-mine aria-pressed:text-accent disabled:cursor-default disabled:opacity-55";

function replyTarget(row: Row, v: View): ReplyTarget | null {
  const parent = row.entry.parent ? v.rows.find((r) => r.id === row.entry.parent) : undefined;
  if (!parent) return null;
  const who = parent.entry.kind === "note" ? parent.entry.label : parent.entry.sender;
  const when = [parent.entry.date, parent.entry.time].filter(Boolean).join(" ");
  return {
    anchor: parent.id,
    who,
    whoTitle: parent.entry.kind === "note" ? undefined : v.whoTitle(who ?? ""),
    when,
  };
}

/** Adapts a spec row to `Message`; system notes have no sender or bubble so are drawn here. */
function EntryBlock({
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
        className={`sys scroll-mt-6 mx-auto my-3 max-w-[44rem] border border-dashed border-line rounded-[10px] bg-quote px-4 py-2 text-center${row.isChainStart ? " chstart" : ""}${mark === "new" ? " border-l-[3px_solid_var(--o1)]" : ""}`}
        id={row.id}
        data-ch={row.lane}
        style={grid}
      >
        <div className="mb-0.5 text-[.68rem] tabular-nums text-muted">
          <a
            className="rounded-[3px] text-inherit underline-offset-2 decoration-accent no-underline hover:text-accent hover:underline hover:decoration-dotted focus-visible:outline focus-visible:outline-[1.5px] focus-visible:outline-accent focus-visible:outline-offset-1"
            href={`#${row.id}`}
            title="Link to this note"
          >
            {e.date}
          </a>
        </div>
        <div className="mb-1 text-[.75rem] font-bold uppercase tracking-[.08em] text-muted">
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

function Chains({ v }: { v: View }) {
  return (
    <>
      {v.layout.chains.map((c) => (
        <div
          key={`spine-${c.root}`}
          className="spine"
          style={{ gridColumn: c.lane + 1, gridRow: `${c.firstRow}/${c.lastRow + 1}` }}
        />
      ))}
      {v.layout.chains.map((c) => (
        <div
          key={`sec-${c.root}`}
          className="chsec"
          style={{ gridColumn: c.lane + 1, gridRow: `${c.firstRow}/${c.lastRow + 1}` }}
        >
          <div className="chdr" title={v.whoTitle(c.opener)}>
            <b>{c.subject ?? c.opener}</b>
            <span>
              {c.subject ? `${c.opener} · ` : ""}
              {c.date} · {c.entries.length} message{c.entries.length === 1 ? "" : "s"}
            </span>
          </div>
        </div>
      ))}
    </>
  );
}

export default function Timeline({
  spec,
  marks,
  prevLabel,
  filter,
  onShowSpec,
  onRefresh,
  onAdd,
  onEval,
  onPull,
  pulling,
  mediaBase,
  refreshing,
}: {
  spec: Spec;
  /** entry id -> what changed since a previous render, from `--since` */
  marks?: Map<string, "new" | "revised">;
  prevLabel?: string;
  /** supplied by the app; absent in the static export, which cannot re-derive */
  filter?: ThreadFilter;
  /** app-only: the static export has no place to put an interactive panel */
  onShowSpec?: () => void;
  /** app-only: brings a saved page up to date from the corpus (refresh.go). */
  onRefresh?: () => void;
  /** app-only: opens a search to add another email's thread to this page. */
  onAdd?: () => void;
  /** app-only: opens the proposal evaluator, when the last refresh proposed chains. */
  onEval?: () => void;
  /** App-only, and only on a host started with -media. */
  onPull?: (extId: string) => void;
  /** the ext id whose files are being fetched, so its button says so and no second pull starts */
  pulling?: string | null;
  /** Absent in the static export, which has no server. */
  mediaBase?: string;
  refreshing?: boolean;
}) {
  const v = derive(spec);
  const s = v.spec;
  // gmailId -> first row holding it, so unspooled source lines can anchor on this page.
  const anchorByGmail = new Map<string, string>();
  for (const r of v.rows) {
    if (r.entry.gmailId && !anchorByGmail.has(r.entry.gmailId))
      anchorByGmail.set(r.entry.gmailId, r.id);
  }
  return (
    <>
      {v.avatarCss ? <style dangerouslySetInnerHTML={html(v.avatarCss)} /> : null}
      <div className="fixed top-2 right-[calc(var(--panel)+.6rem)] z-[31] flex gap-1.5 max-[1024px]:right-[.6rem] print:hidden">
        <button
          className={toolbarButtonClasses}
          id="viewtog"
          type="button"
          aria-pressed="false"
          aria-label="Thread columns view"
        >
          columns
        </button>
        {onShowSpec ? (
          <button
            className={toolbarButtonClasses}
            id="spectog"
            type="button"
            onClick={onShowSpec}
            aria-label="Show the spec as JSON"
          >
            json
          </button>
        ) : null}
        {onRefresh ? (
          <button
            className={toolbarButtonClasses}
            id="refreshtog"
            type="button"
            onClick={onRefresh}
            disabled={refreshing}
            aria-label="Re-derive this page from the corpus"
          >
            {refreshing ? "refreshing…" : "refresh"}
          </button>
        ) : null}
        {onAdd ? (
          <button
            className={toolbarButtonClasses}
            type="button"
            onClick={onAdd}
            aria-label="Search the corpus for another email to add to this page"
          >
            add email
          </button>
        ) : null}
        {onEval ? (
          <button
            className={toolbarButtonClasses}
            type="button"
            onClick={onEval}
            aria-label="Evaluate chains the queries proposed"
          >
            eval
          </button>
        ) : null}
        <button
          className={toolbarButtonClasses}
          id="maptog"
          type="button"
          aria-pressed="true"
          aria-label="Reply tree panel"
        >
          tree
        </button>
        <button
          className={toolbarButtonClasses}
          id="plaintog"
          type="button"
          aria-pressed="false"
          aria-label="Ignore the sender's own formatting"
        >
          plain
        </button>
      </div>
      <div className="wrap mx-auto max-w-[76rem] px-5 pt-7 pb-14">
        <header className="top mb-1 border-b border-line pb-3">
          <h1 className="m-0 mb-1 text-[1.3rem] tracking-[-.01em]">
            {v.hashed ? <span className="text-muted font-normal">#</span> : null}
            {v.hashed ? v.title.slice(1) : v.title}
          </h1>
          <p
            className="mb-2 text-muted text-[.86rem]"
            dangerouslySetInnerHTML={html(s.subtitle ?? `${s.messages.length} messages.`)}
          />
          <Legend />
          <ParticipantsPanel v={v} open />
          {marks ? (
            <DiffPanel v={v} marks={marks} prevLabel={prevLabel ?? "the previous run"} />
          ) : null}
          <SourcesPanel v={v} filter={filter} />
        </header>
        <div className="stream" id="stream" style={{ ["--nch" as string]: v.layout.laneCount }}>
          <Chains v={v} />
          {v.rows.map((r) => (
            <EntryBlock
              key={r.id}
              row={r}
              v={v}
              mark={marks?.get(r.id)}
              anchorByGmail={anchorByGmail}
              onPull={onPull}
              pulling={pulling}
              mediaBase={mediaBase}
            />
          ))}
        </div>
        {s.openItems?.length ? (
          <footer className="end mt-8 border-t border-line pt-4">
            <h2 className="mb-2 mt-0 text-[.8rem] uppercase tracking-[.1em] text-muted">
              {s.openItemsTitle ?? "Still open"}
            </h2>
            <ul className="m-0 pl-5">
              {s.openItems.map((i, n) => (
                <li className="my-1 text-[.89rem]" key={n} dangerouslySetInnerHTML={html(i)} />
              ))}
            </ul>
          </footer>
        ) : null}
      </div>
      <Minimap v={v} />
    </>
  );
}
