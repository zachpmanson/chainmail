import { Fragment } from "react";
import type { Entry } from "../lib/spec";
import { derive, initials, type Row, type View } from "../lib/derive";
import type { Timeline as Spec } from "../lib/spec";
import { COLLAPSE_FROM, msgCount, provenance, type SourceId } from "../lib/sources";
import { attHref, hasPreview } from "../lib/attachments";
import { DiffPanel, Legend, ParticipantsPanel, SourcesPanel, type ChainFilter } from "./Panels";
import { Minimap } from "./Minimap";

const html = (s: string) => ({ __html: s });

function Avatar({ row, v }: { row: Row; v: View }) {
  const name = row.entry.sender ?? "";
  const pic = row.avatarClass;
  return (
    <div className={`av ${row.orgSlot}${pic ? ` pic ${pic}` : ""}`} title={v.whoTitle(name)}>
      {pic ? null : <span className="ini">{initials(name)}</span>}
    </div>
  );
}

/**
 * A zone is shown three ways, because the reader's next move differs in each.
 * Stated is a fact and reads as one. Inferred is a claim and is dotted, dimmed
 * and suffixed so it cannot be mistaken for the source's own words. Unknown is
 * neither, and is said out loud rather than left as whitespace — an unlabelled
 * clock beside a labelled one silently invites the reader to compare them, and
 * on this page most clocks are unlabelled.
 */
function Stamp({ row }: { row: Row }) {
  const { date, time, tz, zone } = row.stamp;
  return (
    <a className="tm pl" href={`#${row.id}`} title="Link to this message">
      {date}
      {time ? ` · ${time}` : ""}
      {zone === "stated" ? <span className="tz">{tz}</span> : null}
      {zone === "inferred" ? (
        <span
          className="tz tzi"
          title="Inferred — this source stated no zone. The offset was worked out from the client that quoted this message; see the source notes."
        >{` ${tz}?`}</span>
      ) : null}
      {zone === "unknown" ? (
        <span
          className="tz tzu"
          title="Zone unknown — this source stated none and nothing available places it. The clock is a wall clock as quoted, so it cannot be compared with the times above and below it."
        >
          {" zone unknown"}
        </span>
      ) : null}
    </a>
  );
}

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

function Attachments({ e }: { e: Entry }) {
  if (!e.attachments?.length) return null;
  return (
    <div className="atts">
      <span className="clip">attached</span>
      {e.attachments.map((a, i) => {
        const href = attHref(a);
        const thumb = hasPreview(a) ? (
          <img
            className="athumb"
            src={a.preview}
            width={a.previewW}
            height={a.previewH}
            /* Decorative here: the filename beside it already names the file, so
               announcing it twice only makes the chip longer to listen to. */
            alt=""
          />
        ) : null;
        const label = (
          <>
            {thumb}
            <span className="afn">{a.name}</span>
            <span className="ameta">
              {a.kind ?? "file"} · {a.size ?? ""}
            </span>
          </>
        );
        // The chip stays the same link it always was, and the popover is layered
        // onto it by script. That is deliberate: no new control appears, the
        // trigger is already in the tab order, and with scripting unavailable
        // the click still opens the attachment at its source rather than doing
        // nothing. data-pop marks which chips script should intercept.
        return href ? (
          <a
            key={i}
            className={thumb ? "att haspop" : "att"}
            href={href}
            target="_blank"
            rel="noopener"
            {...(thumb
              ? { "data-pop": a.name, "aria-haspopup": "dialog" as const }
              : {})}
          >
            {label}
          </a>
        ) : (
          // A thumbnail still earns its place on a chip with nowhere to go — it
          // is the only thing here that says what the file actually is. It gets
          // no popover, though: the only way to offer one would be a control
          // that does nothing at all without scripting.
          <span key={i} className="att nolink">
            {label}
          </span>
        );
      })}
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
function SourceIds({ ids }: { ids: SourceId[] }) {
  return (
    <>
      {ids.map((s, i) => (
        <Fragment key={i}>
          {i ? ", " : ""}
          <span className="sid">
            {s.gmailId ? (
              <a
                href={`https://mail.google.com/mail/u/0/#all/${s.gmailId}`}
                target="_blank"
                rel="noopener"
              >
                {s.text}
              </a>
            ) : (
              s.text
            )}
          </span>
        </Fragment>
      ))}
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
function Source({ e }: { e: Entry }) {
  if (!e.source) return null;
  const p = provenance(e.source);
  if (p.kind === "prose") return <span className="src">{p.text}</span>;
  if (p.ids.length < COLLAPSE_FROM) {
    return (
      <span className="src">
        {p.prefix}
        <SourceIds ids={p.ids} />
      </span>
    );
  }
  return (
    <details className="src srcx">
      <summary>
        {p.prefix}
        {msgCount(p.ids.length)}
      </summary>
      <div className="srcids">
        <SourceIds ids={p.ids} />
      </div>
    </details>
  );
}

function EntryBlock({ row, v, mark }: { row: Row; v: View; mark?: "new" | "revised" }) {
  const e = row.entry;
  const grid = { gridColumn: row.lane + 1, gridRow: row.row };
  const start = row.isChainStart ? " chstart" : "";

  if (e.kind === "note") {
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
        <div className="bd" dangerouslySetInnerHTML={html(e.body)} />
        <ReplyLink row={row} v={v} />
      </div>
    );
  }

  // The org slot rides on the row so a bubble can carry its sender's colour.
  // Scanning a page happens in the body column, not the avatar column, so the
  // avatar alone leaves the org unreadable exactly where the eye already is.
  const cls = ["msg", row.orgSlot, e.me && "me", e.quoted && "q",
    row.isChainStart && "chstart", mark === "new" && "isnew"]
    .filter(Boolean)
    .join(" ");
  return (
    <div className={cls} id={row.id} data-ch={row.lane} style={grid}>
      <div className="col">
        <div className="hdr">
          <Avatar row={row} v={v} />
          <span className="nm" title={v.whoTitle(e.sender ?? "")}>
            {e.sender}
          </span>
          <span className="org">{e.org}</span>
          <Stamp row={row} />
          {mark === "new" ? <span className="newpill">new</span> : null}
          {mark === "revised" ? <span className="revpill">revised</span> : null}
        </div>
        <div className="bub">
          {e.mentions?.length ? (
            <div className="ment">
              {e.mentions.map((m) => (
                <span className="at" key={m}>
                  @{m}
                </span>
              ))}
            </div>
          ) : null}
          <div className="bd" dangerouslySetInnerHTML={html(e.body)} />
          <Attachments e={e} />
          <div className="foot">
            <span className="to">to {e.to ?? "—"}</span>
            <ReplyLink row={row} v={v} />
            <Source e={e} />
          </div>
        </div>
      </div>
    </div>
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
}

export function Timeline({ spec, marks, prevLabel, filter, onShowSpec }: TimelineProps) {
  const v = derive(spec);
  const s = v.spec;
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
          <EntryBlock key={r.id} row={r} v={v} mark={marks?.get(r.id)} />
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
