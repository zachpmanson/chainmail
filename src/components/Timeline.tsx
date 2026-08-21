import type { Entry } from "../lib/spec";
import { derive, initials, type Row, type View } from "../lib/derive";
import type { Timeline as Spec } from "../lib/spec";
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
        const label = (
          <>
            <span className="afn">{a.name}</span>
            <span className="ameta">
              {a.kind ?? "file"} · {a.size ?? ""}
            </span>
          </>
        );
        return a.gmailId ? (
          <a
            key={i}
            className="att"
            href={`https://mail.google.com/mail/u/0/#all/${a.gmailId}`}
            target="_blank"
            rel="noopener"
          >
            {label}
          </a>
        ) : (
          <span key={i} className="att nolink">
            {label}
          </span>
        );
      })}
    </div>
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
        <div dangerouslySetInnerHTML={html(e.body)} />
        <ReplyLink row={row} v={v} />
      </div>
    );
  }

  const cls = ["msg", e.me && "me", e.quoted && "q", row.isChainStart && "chstart",
    mark === "new" && "isnew"]
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
          <div dangerouslySetInnerHTML={html(e.body)} />
          <Attachments e={e} />
          <div className="foot">
            <span className="to">to {e.to ?? "—"}</span>
            <ReplyLink row={row} v={v} />
            {e.source ? <span className="src">{e.source}</span> : null}
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
