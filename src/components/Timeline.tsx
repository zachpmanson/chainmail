import { Fragment } from "react";
import { derive, type Row, type RowEdit, type View } from "../lib/derive";
import type { Timeline as Spec } from "../lib/spec";
import { COLLAPSE_FROM, msgCount, provenance, type SourceId } from "../lib/sources";
import { DiffPanel, Legend, ParticipantsPanel, SourcesPanel, type ChainFilter } from "./Panels";
import { Minimap } from "./Minimap";
import { Message } from "./Message";
import { trimBody } from "../lib/trimBody";

const html = (s: string) => ({ __html: s });

function ReplyLink({ row, v }: { row: Row; v: View }) {
  const parent = row.entry.parent
    ? v.rows.find((r) => r.id === row.entry.parent)
    : undefined;
  if (!parent) return <span className="tstart">thread start</span>;
  const who = parent.entry.kind === "note" ? parent.entry.label : parent.entry.sender;
  const when = [parent.entry.date, parent.entry.time].filter(Boolean).join(" ");
  return (
    <a className="par" href={`#${parent.id}`} title={`In reply to ${who}, ${when}`}>
      <span className="arw">&#8617;</span>
      <span className="parlbl">
        in reply to <b>{who}</b>, {when}
      </span>
    </a>
  );
}

/** A quoter's inline edit to a message this one quoted (issue #42): the
 *  modified text with the change marked, anchored to the original, attributed
 *  to the quoter — rendered here so the edit reads inside the message that
 *  made it rather than floating as its own unspooled node. */
function Edits({ edits, v }: { edits?: RowEdit[]; v: View }) {
  if (!edits?.length) return null;
  return (
    <div className="edits">
      {edits.map((ed, i) => (
        <div className="edit" key={ed.base || i}>
          <div className="ehdr">
            edited by <span className="editwho">{ed.who || v.title || "someone"}</span>
            {ed.origWho || ed.origStamp ? (
              <>
                ,{" "}
                <a href={`#${ed.base}`} title="the message this change was made to">
                  original
                </a>
                {ed.origWho ? <span> from {ed.origWho}</span> : null}
                {ed.origStamp ? <span className="ets"> at {ed.origStamp}</span> : null}
              </>
            ) : null}
          </div>
          <div className="ebd" dangerouslySetInnerHTML={html(ed.html)} />
        </div>
      ))}
    </div>
  );
}

/**
 * The ids on a provenance line, comma-run, each openable where it can be.
 *
 * The separator sits outside .sid so that the only place the line may break is
 * after a comma: inside .sid, "msg" and its handle are one token to the reader
 * and splitting them across lines reads as two truncated ids.
 */
function SourceIds({ ids, unspooled, anchorByGmail }: {
  ids: SourceId[];
  /** the line is "unspooled from …"; its ids name the message the content was lifted out of */
  unspooled: boolean;
  /** gmailId -> this page's anchor for that message, where it is present as a row */
  anchorByGmail: Map<string, string>;
}) {
  return (
    <>
      {ids.map((s, i) => {
        // An unspooled id names a sibling message on this very page, so it links
        // there (a fragment anchor) instead of shipping the reader out to Gmail.
        // A direct message's own id still opens its mailbox copy.
        const anchor = unspooled && s.gmailId ? anchorByGmail.get(s.gmailId) : undefined;
        return (
          <Fragment key={i}>
            {i ? ", " : ""}
            <span className="sid">
              {anchor ? (
                <a href={`#${anchor}`} title="The message this was unspooled from, on this page">
                  {s.text}
                </a>
              ) : unspooled || !s.gmailId ? (
                s.text
              ) : (
                <a
                  href={`https://mail.google.com/mail/u/0/#all/${s.gmailId}`}
                  target="_blank"
                  rel="noopener"
                >
                  {s.text}
                </a>
              )}
            </span>
          </Fragment>
        );
      })}
    </>
  );
}

/**
 * Where an entry was found. The ids are the useful part of the line — each names
 * a message the reader can open — so a collapsed line says how many there are
 * and keeps every id in the document, rather than summarising them away.
 *
 * A native <details>, matching the panels above, and not a scripted toggle: the
 * exported page is meant to be readable with scripting disabled, and <details>
 * is keyboard-operable and reachable by find-in-page without any of ours. A
 * folding mechanism elsewhere on the page can be the same element.
 */
function Source({ source, anchorByGmail }: { source?: string; anchorByGmail: Map<string, string> }) {
  if (!source) return null;
  const p = provenance(source);
  if (p.kind === "prose") return <span className="src">{p.text}</span>;
  // "unspooled from …" lines carry an empty prefix only when not unspooled;
  // prose never reaches here, so prefix !== "" means the ids were unspooled
  const unspooled = p.prefix !== "";
  const ids = <SourceIds ids={p.ids} unspooled={unspooled} anchorByGmail={anchorByGmail} />;
  if (p.ids.length < COLLAPSE_FROM) {
    return (
      <span className="src">
        {p.prefix}
        {ids}
      </span>
    );
  }
  return (
    <details className="src srcx">
      <summary>
        {p.prefix}
        {msgCount(p.ids.length)}
      </summary>
      <div className="srcids">{ids}</div>
    </details>
  );
}

/**
 * One row of the transcript: a spec `Row` and `View` resolved into a `Message`.
 *
 * This is the adapter, and this file is the only place that knows both halves.
 * Everything a bubble cannot be drawn without goes over as data — the sender,
 * the org slot, the attachments, the clock, the grid position. Everything that
 * takes the spec to work out goes over as a node: the reply link (which resolves
 * the parent through the reply graph), the provenance line (which resolves ids to
 * anchors on this page), a quoter's inline edit (which resolves a diff against
 * the message it was made to), and the payload the copy button puts on the
 * clipboard.
 *
 * A system note is not a message and is drawn here rather than through `Message`.
 * It has no sender, no bubble, no org colour and no attachments, so pushing it
 * through the bubble component would mean a component reaching every bubble part
 * with nothing to put in it — and a `.sys` that rendering-inspected an empty
 * sender to decide whether it was a note at all.
 */
function EntryBlock({ row, v, mark, anchorByGmail, onPull, pulling, mediaBase }: {
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
    const start = row.isChainStart ? " chstart" : "";
    return (
      <div
        className={`sys${start}${mark === "new" ? " isnew" : ""}`}
        id={row.id}
        data-ch={row.lane}
        style={grid}
      >
        <div className="sysday">
          <a className="pl" href={`#${row.id}`} title="Link to this note">
            {e.date}
          </a>
        </div>
        <div className="syslabel">{e.label}</div>
        <div className="bd" dangerouslySetInnerHTML={html(trimBody(e.body))} />
        <ReplyLink row={row} v={v} />
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
      stamp={row.stamp}
      style={grid}
      lane={row.lane}
      chainStart={row.isChainStart}
      mark={mark}
      reply={<ReplyLink row={row} v={v} />}
      edits={<Edits edits={row.edits} v={v} />}
      source={<Source source={e.source} anchorByGmail={anchorByGmail} />}
      /* The spec entry as the renderer saw it, plus the row id and any resolved
         quote-edits (the "edited by … original from …" attribution), so a message
         that renders wrong can be pasted somewhere and inspected whole. */
      copyJson={{
        id: row.id,
        chain: row.chain ?? null,
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

export interface TimelineProps {
  spec: Spec;
  /** entry id -> what changed since a previous render, from `--since` */
  marks?: Map<string, "new" | "revised">;
  prevLabel?: string;
  /** supplied by the app; absent in the static export, which cannot re-derive */
  filter?: ChainFilter;
  /** app-only: the static export has no place to put an interactive panel */
  onShowSpec?: () => void;
  /** app-only: brings a saved page up to date from the corpus (refresh.go). */
  onRefresh?: () => void;
  /** app-only: opens a search to add another email's chain to this page. */
  onAdd?: () => void;
  /** app-only: opens the proposal evaluator, when the last refresh proposed chains. */
  onEval?: () => void;
  /**
   * app-only, and only on a host that was started with -media: fetch one
   * message's attachment bytes, so a chip under it can become the file rather
   * than a link back to Gmail for it.
   */
  onPull?: (extId: string) => void;
  /** the ext id whose files are being fetched, so its button says so and no second pull starts */
  pulling?: string | null;
  /**
   * Where the app serves stored attachment bytes. Absent in the static export,
   * which has no server to serve them: a shared page keeps its source links.
   */
  mediaBase?: string;
  refreshing?: boolean;
}

export function Timeline({ spec, marks, prevLabel, filter, onShowSpec, onRefresh, onAdd, onEval, onPull, pulling, mediaBase, refreshing }: TimelineProps) {
  const v = derive(spec);
  const s = v.spec;
  // gmailId -> the id of the row that carries it, so an unspooled source line
  // can anchor to the message it was lifted out of on this same page. A message
  // is keyed by the first row that holds its gmailId.
  const anchorByGmail = new Map<string, string>();
  for (const r of v.rows) {
    if (r.entry.gmailId && !anchorByGmail.has(r.entry.gmailId)) anchorByGmail.set(r.entry.gmailId, r.id);
  }
  return (
    <>
      {v.avatarCss ? <style dangerouslySetInnerHTML={html(v.avatarCss)} /> : null}
      <div className="toolbar">
        <button className="tbtn" id="viewtog" type="button" aria-pressed="false"
                aria-label="Chain columns view">columns</button>
        {onShowSpec ? (
          <button className="tbtn" id="spectog" type="button" onClick={onShowSpec}
                  aria-label="Show the spec as JSON">json</button>
        ) : null}
        {onRefresh ? (
          <button className="tbtn" id="refreshtog" type="button" onClick={onRefresh}
                  disabled={refreshing}
                  aria-label="Re-derive this page from the corpus">
            {refreshing ? "refreshing…" : "refresh"}
          </button>
        ) : null}
        {onAdd ? (
          <button className="tbtn" type="button" onClick={onAdd}
                  aria-label="Search the corpus for another email to add to this page">
            add email
          </button>
        ) : null}
        {onEval ? (
          <button className="tbtn" type="button" onClick={onEval}
                  aria-label="Evaluate chains the queries proposed">eval</button>
        ) : null}
        <button className="tbtn" id="maptog" type="button" aria-pressed="true"
                aria-label="Reply tree panel">tree</button>
        <button className="tbtn" id="plaintog" type="button" aria-pressed="false"
                aria-label="Ignore the sender's own formatting">plain</button>
      </div>
      <div className="wrap">
      <header className="top">
        <h1>
          {v.hashed ? <span className="hash">#</span> : null}
          {v.hashed ? v.title.slice(1) : v.title}
        </h1>
        <p className="sub" dangerouslySetInnerHTML={html(s.subtitle ?? `${s.messages.length} messages.`)} />
        <Legend />
        <ParticipantsPanel v={v} />
        {marks ? <DiffPanel v={v} marks={marks} prevLabel={prevLabel ?? "the previous run"} /> : null}
        <SourcesPanel v={v} filter={filter} />
      </header>
      <div className="stream" id="stream" style={{ ["--nch" as string]: v.layout.laneCount }}>
        <Chains v={v} />
        {v.rows.map((r) => (
          <EntryBlock key={r.id} row={r} v={v} mark={marks?.get(r.id)} anchorByGmail={anchorByGmail}
                      onPull={onPull} pulling={pulling} mediaBase={mediaBase} />
        ))}
      </div>
      {s.openItems?.length ? (
        <footer className="end">
          <h2>{s.openItemsTitle ?? "Still open"}</h2>
          <ul>
            {s.openItems.map((i, n) => (
              <li key={n} dangerouslySetInnerHTML={html(i)} />
            ))}
          </ul>
        </footer>
      ) : null}
      </div>
      <Minimap v={v} />
    </>
  );
}
