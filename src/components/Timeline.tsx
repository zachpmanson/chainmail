import type { Entry } from "../lib/spec";
import { derive, initials, type Row, type View } from "../lib/derive";
import type { Timeline as Spec } from "../lib/spec";

const html = (s: string) => ({ __html: s });

function Avatar({ row, v }: { row: Row; v: View }) {
  const name = row.entry.sender ?? "";
  const cls = `av ${row.orgSlot}${row.avatar ? " pic" : ""}`;
  return (
    <div
      className={cls}
      style={row.avatar ? { backgroundImage: `url(${row.avatar})` } : undefined}
      title={v.whoTitle(name)}
    >
      {row.avatar ? null : <span className="ini">{initials(name)}</span>}
    </div>
  );
}

function Stamp({ row }: { row: Row }) {
  const { date, time, tz, inferred } = row.stamp;
  return (
    <a className="tm pl" href={`#${row.id}`} title="Link to this message">
      {date}
      {time ? ` · ${time}` : ""}
      {tz ? (
        inferred ? (
          <span
            className="tz tzi"
            title="Inferred — this source stated no timezone. Ordering and this label come from the zones this sender stated elsewhere."
          >{` ${tz}?`}</span>
        ) : (
          <span className="tz">{tz}</span>
        )
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

function EntryBlock({ row, v }: { row: Row; v: View }) {
  const e = row.entry;
  const grid = { gridColumn: row.lane + 1, gridRow: row.row };
  const start = row.isChainStart ? " chstart" : "";

  if (e.kind === "note") {
    return (
      <div className={`sys${start}`} id={row.id} data-ch={row.lane} style={grid}>
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

  const cls = ["msg", e.me && "me", e.quoted && "q", row.isChainStart && "chstart"]
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

export function Timeline({ spec }: { spec: Spec }) {
  const v = derive(spec);
  const s = v.spec;
  return (
    <div className="wrap">
      <header className="top">
        <h1>
          {v.hashed ? <span className="hash">#</span> : null}
          {v.hashed ? v.title.slice(1) : v.title}
        </h1>
        <p className="sub" dangerouslySetInnerHTML={html(s.subtitle ?? `${s.messages.length} messages.`)} />
        <p className="roster">
          {v.orgs.map((o) => (
            <span key={o}>
              <b>{o}</b>{" "}
              {[...new Set(s.messages.filter((m) => m.org === o).map((m) => m.sender))]
                .filter(Boolean)
                .join(" · ")}
              {"  "}
            </span>
          ))}
        </p>
      </header>
      <div className="stream" id="stream" style={{ ["--nch" as string]: v.layout.laneCount }}>
        <Chains v={v} />
        {v.rows.map((r) => (
          <EntryBlock key={r.id} row={r} v={v} />
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
  );
}
