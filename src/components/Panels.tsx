import { initials, type View } from "../lib/derive";
import type { Timeline as Spec } from "../lib/spec";

/** A participant as the panel needs it, however the spec supplied them. */
type Person = NonNullable<Spec["participants"]>[number];
type Thread = NonNullable<Spec["threads"]>[number];

const html = (s: string) => ({ __html: s });

function Face({ name, v }: { name: string; v: View }) {
  const row = v.rows.find((r) => r.entry.sender === name);
  const slot = row?.orgSlot ?? "o5";
  // reuses the per-avatar CSS rule rather than inlining the image again
  const pic = v.rows.find((r) => r.entry.sender === name)?.avatarClass;
  return (
    <div className={`av ${slot}${pic ? ` pic ${pic}` : ""}`}>
      {pic ? null : <span className="ini">{initials(name)}</span>}
    </div>
  );
}

/**
 * Who is in the trail, with addresses. Auto-derives from senders when the spec
 * omits `participants`, but a spec should pass them explicitly: the derivation
 * can only see people who sent something, and a trail's cast is always larger
 * than its set of senders.
 */
export function ParticipantsPanel({ v }: { v: View }) {
  const s = v.spec;
  const people: Person[] =
    s.participants ??
    [...new Map(v.rows.filter((r) => r.entry.sender).map((r) => [r.entry.sender!, r])).values()].map(
      (r): Person => ({ name: r.entry.sender!, org: r.entry.org, email: r.entry.fromEmail }),
    );

  const stats = new Map<string, { n: number }>();
  for (const r of v.rows) {
    if (!r.entry.sender) continue;
    stats.set(r.entry.sender, { n: (stats.get(r.entry.sender)?.n ?? 0) + 1 });
  }

  const groups: { org: string; people: Person[] }[] = [];
  for (const p of people) {
    const org = p.org ?? "";
    const g = groups.find((x) => x.org === org);
    if (g) g.people.push(p);
    else groups.push({ org, people: [p] });
  }

  return (
    <details className="pan people" open>
      <summary>Participants ({people.length})</summary>
      <div className="pbody">
        <div className="who">
          {groups.map((g) => (
            <div key={g.org || "other"} style={{ display: "contents" }}>
              <div className="ogh">{g.org || "Other"}</div>
              {g.people.map((p, i) => {
                const n = stats.get(p.name)?.n;
                // The note is shown alongside the count, not only in its absence:
                // it says how a person was seen, which is exactly the thing a
                // count of their messages does not tell you.
                const bits = [
                  p.role,
                  n ? `${n} msg${n === 1 ? "" : "s"}` : undefined,
                  p.note,
                ].filter(Boolean);
                // Keyed by position, because a name is not unique: two corpus
                // people can carry one display name, and both are listed rather
                // than one silently winning.
                return (
                  <div className="p1" key={i}>
                    <div className="pd">
                      <div className="pn">
                        <Face name={p.name} v={v} />
                        <span title={v.whoTitle(p.name)}>{p.name}</span>
                      </div>
                      {p.email ? (
                        <a className="pe" href={`mailto:${p.email}`}>
                          {p.email}
                        </a>
                      ) : (
                        <span className="pr">address not in the trail</span>
                      )}
                      <div className="pr">{bits.join(" · ")}</div>
                    </div>
                  </div>
                );
              })}
            </div>
          ))}
        </div>
      </div>
    </details>
  );
}

export interface ChainFilter {
  /** every chain in the unfiltered trail, so excluded ones stay listed */
  chains: {
    root: string;
    subject?: string;
    opener: string;
    date: string;
    count: number;
    /** mailbox id of the chain's thread, where any entry in it names one */
    gmailId?: string;
    /** anchor of the chain's first entry, for jumping to it in the page */
    anchor: string;
  }[];
  /** chain roots currently excluded from the view */
  excluded: Set<string>;
  onToggle: (root: string) => void;
}

/**
 * Everything the page was built from: chains, searches, threads, attachments,
 * caveats. When a filter is supplied, each chain gets a checkbox — a trail often
 * picks up a thread that turns out not to belong, and dropping it should re-lay
 * the page rather than just blank out rows.
 */
export function SourcesPanel({ v, filter }: { v: View; filter?: ChainFilter }) {
  const s: Spec = v.spec;
  const groups: { title: string; items: React.ReactNode[] }[] = [];

  if (filter) {
    groups.push({
      title: `Chains (${filter.chains.length})`,
      items: filter.chains.map((c) => (
        <label className="chk" key={c.root} data-chain={c.root}>
          <input
            type="checkbox"
            checked={!filter.excluded.has(c.root)}
            onChange={() => filter.onToggle(c.root)}
          />
          <span>
            {c.subject ?? c.opener}
            {c.gmailId ? (
              <>
                {" "}
                <a
                  className="srclink"
                  href={`https://mail.google.com/mail/u/0/#all/${c.gmailId}`}
                  target="_blank"
                  rel="noopener"
                  title="Open this thread in Gmail"
                  onClick={(e) => e.stopPropagation()}
                >
                  mail
                </a>
              </>
            ) : null}{" "}
            <a
              className="srclink"
              href={`#${c.anchor}`}
              title="Jump to the start of this chain"
              onClick={(e) => e.stopPropagation()}
            >
              start
            </a>
            <span className="note">
              {" "}
              — {c.opener}, {c.date} · {c.count} message{c.count === 1 ? "" : "s"}
            </span>
          </span>
        </label>
      )),
    });
  }

  if (s.queries?.length) {
    groups.push({
      title: `Searches run (${s.queries.length})`,
      items: s.queries.map((q, i) => {
        const [text, note] = typeof q === "string" ? [q, undefined] : [q.q, q.note];
        return (
          <span key={i}>
            <span className="qy">{text}</span>
            {note ? <span className="note"> — {note}</span> : null}
          </span>
        );
      }),
    });
  }

  // fall back to the threads the entries themselves name; a hand-written list
  // carries count/span/note, which is what makes this panel worth reading
  const threads: Thread[] =
    s.threads ??
    [...new Map(v.rows.filter((r) => r.entry.threadId ?? r.entry.gmailId)
      .map((r) => [r.entry.threadId ?? r.entry.gmailId!, r])).entries()]
      .map(([id, r]): Thread => ({ id, subject: r.entry.subject ?? "(thread)" }));
  if (threads.length) {
    groups.push({
      title: `Mail threads (${threads.length})`,
      items: threads.map((t, i) => {
        const meta = [t.count ? `${t.count} msgs` : null, t.span, t.note].filter(Boolean).join(" · ");
        const label = t.subject ?? "(thread)";
        return (
          <span key={i}>
            {t.id ? (
              <a href={`https://mail.google.com/mail/u/0/#all/${t.id}`} target="_blank" rel="noopener">
                {label}
              </a>
            ) : (
              label
            )}
            {meta ? <span className="note"> — {meta}</span> : null}
          </span>
        );
      }),
    });
  }

  const atts = v.rows.flatMap((r) => (r.entry.attachments ?? []).map((a) => ({ a, r })));
  if (atts.length) {
    groups.push({
      title: `Attachments (${atts.length})`,
      items: atts.map(({ a }, i) => (
        <span key={i}>
          {a.gmailId ? (
            <a href={`https://mail.google.com/mail/u/0/#all/${a.gmailId}`} target="_blank" rel="noopener">
              <code>{a.name}</code>
            </a>
          ) : (
            <code>{a.name}</code>
          )}
          <span className="note">
            {" "}
            — {a.kind ?? "file"}, {a.size ?? ""}
          </span>
        </span>
      )),
    });
  }

  for (const n of s.sourceNotes ?? []) {
    groups.push({ title: n.title, items: n.items.map((i, k) => <span key={k}>{i}</span>) });
  }

  if (!groups.length) return null;
  return (
    <details className="pan sources">
      <summary>Sources &amp; provenance</summary>
      <div className="pbody">
        {groups.map((g) => (
          // each group collapses on its own: the titles carry counts, so a closed
          // group still tells you what is in it
          <details className="srcgrp" key={g.title} open>
            <summary>
              <span className="srch">{g.title}</span>
            </summary>
            <ul>
              {g.items.map((it, i) => (
                <li key={i}>{it}</li>
              ))}
            </ul>
          </details>
        ))}
      </div>
    </details>
  );
}

/** The four bubble states, so the page explains its own notation. */
export function Legend() {
  return (
    <div className="states">
      <div className="st">
        <span className="sw plain" />
        <div>
          <b>Solid</b> — a real standalone message in the mailbox. Footer carries its Gmail
          message&nbsp;id.
        </div>
      </div>
      <div className="st">
        <span className="sw dash" />
        <div>
          <b>Dashed</b> — reconstructed from quoted text inside a later email; no message of
          its own. Footer says which email it came out of, and its timestamp is the one in the
          quoted header.
        </div>
      </div>
      <div className="st">
        <span className="sw mine" />
        <div>
          <b>Tinted</b> — sent by you.
        </div>
      </div>
      <div className="st">
        <span className="sw clipsw">&#128206;</span>
        <div>
          <b>Attachment</b> — links through to that message in Gmail. Only detectable on real
          mailbox messages, never on reconstructed ones.
        </div>
      </div>
    </div>
  );
}

export { html };

/** What this pass added, relative to the spec recovered from a prior render. */
export function DiffPanel({
  v,
  marks,
  prevLabel,
}: {
  v: View;
  marks: Map<string, "new" | "revised">;
  prevLabel: string;
}) {
  const pick = (kind: "new" | "revised") => v.rows.filter((r) => marks.get(r.id) === kind);
  const fresh = pick("new");
  const revised = pick("revised");

  const list = (rs: typeof fresh) =>
    rs.map((r) => (
      <li key={r.id}>
        <a className="xref" href={`#${r.id}`}>
          <b>{r.entry.kind === "note" ? r.entry.label : r.entry.sender}</b>,{" "}
          {[r.entry.date, r.entry.time].filter(Boolean).join(" ")}
        </a>
        {r.entry.source ? <span className="note"> {"—"} {r.entry.source}</span> : null}
      </li>
    ));

  if (!fresh.length && !revised.length) {
    return (
      <details className="pan" open>
        <summary>Since last run</summary>
        <div className="pbody">
          <div className="srcgrp">
            <ul>
              <li>Nothing new. Every entry on this page was already present in {prevLabel}.</li>
            </ul>
          </div>
        </div>
      </details>
    );
  }

  return (
    <details className="pan" open>
      <summary>
        Since last run {"—"} {fresh.length} new, {revised.length} revised
      </summary>
      <div className="pbody">
        {fresh.length ? (
          <div className="srcgrp">
            <div className="srch">
              New since {prevLabel} ({fresh.length})
            </div>
            <ul>{list(fresh)}</ul>
          </div>
        ) : null}
        {revised.length ? (
          <div className="srcgrp">
            <div className="srch">Revised ({revised.length})</div>
            <ul>{list(revised)}</ul>
          </div>
        ) : null}
      </div>
    </details>
  );
}
