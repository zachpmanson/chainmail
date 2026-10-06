import type { View } from "../lib/derive";
import type { Timeline as Spec } from "../lib/spec";
import { msgCount } from "../lib/sources";
import { attHref } from "../lib/attachments";
import { gmailMessageURL } from "../lib/gmailUrl";
import { Checkbox } from "./Checkbox";

type Thread = NonNullable<Spec["threads"]>[number];

const html = (s: string) => ({ __html: s });

export interface ThreadFilter {
  /** every thread in the unfiltered trail, so excluded ones stay listed */
  chains: {
    root: string;
    subject?: string;
    opener: string;
    date: string;
    count: number;
    /** mailbox id of the thread's thread, where any entry in it names one */
    gmailId?: string;
    /** anchor of the thread's first entry, for jumping to it in the page */
    anchor: string;
  }[];
  /** thread roots currently excluded from the view */
  excluded: Set<string>;
  onToggle: (root: string) => void;
}

/**
 * Everything the page was built from: chains, searches, threads, attachments,
 * caveats. When a filter is supplied, each thread gets a checkbox — a trail often
 * picks up a thread that turns out not to belong, and dropping it should re-lay
 * the page rather than just blank out rows.
 */
export function SourcesPanel({ v, filter }: { v: View; filter?: ThreadFilter }) {
  const s: Spec = v.spec;
  const groups: { title: string; items: React.ReactNode[] }[] = [];

  if (filter) {
    groups.push({
      title: `Chains (${filter.chains.length})`,
      items: filter.chains.map((c) => (
        <label className="chk flex cursor-pointer items-start gap-[.4rem] hover:text-accent" key={c.root} data-chain={c.root}>
          <Checkbox
            accent="accent"
            className="mt-[.15rem] flex-none"
            checked={!filter.excluded.has(c.root)}
            onChange={() => filter.onToggle(c.root)}
          />
          <span>
            {c.subject ?? c.opener}
            {c.gmailId ? (
              <>
                {" "}
                <a
                  className="srclink whitespace-nowrap rounded border border-line px-1 text-[.66rem] font-bold uppercase tracking-[.06em] text-muted no-underline hover:border-accent hover:text-accent"
                  href={gmailMessageURL(c.gmailId)}
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
              className="srclink whitespace-nowrap rounded border border-line px-1 text-[.66rem] font-bold uppercase tracking-[.06em] text-muted no-underline hover:border-accent hover:text-accent"
              href={`#${c.anchor}`}
              title="Jump to the start of this thread"
              onClick={(e) => e.stopPropagation()}
            >
              start
            </a>
            <span className="note text-[.92em] text-muted">
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
            <span className="qy font-mono text-[.9em]">{text}</span>
            {note ? <span className="note text-[.92em] text-muted"> — {note}</span> : null}
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
        const meta = [t.count ? msgCount(t.count) : null, t.span, t.note].filter(Boolean).join(" · ");
        const label = t.subject ?? "(thread)";
        return (
          <span key={i}>
            {t.id ? (
              <a className="text-inherit underline decoration-accent underline-offset-2 hover:text-accent" href={gmailMessageURL(t.id)} target="_blank" rel="noopener">
                {label}
              </a>
            ) : (
              label
            )}
            {meta ? <span className="note text-[.92em] text-muted"> — {meta}</span> : null}
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
          {attHref(a) ? (
            <a className="text-inherit underline decoration-accent underline-offset-2 hover:text-accent" href={attHref(a)} target="_blank" rel="noopener">
              <code>{a.name}</code>
            </a>
          ) : (
            <code>{a.name}</code>
          )}
          <span className="note text-[.92em] text-muted">
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
    <details className="pan sources mt-[.7rem] rounded-[9px] border border-line bg-card">
      <summary className="list-none cursor-pointer px-[.7rem] py-[.4rem] text-[.72rem] font-bold uppercase tracking-[.08em] text-muted hover:text-accent">Sources &amp; provenance</summary>
      <div className="pbody border-t border-line px-[.7rem] pt-[.1rem] pb-[.6rem]">
        {groups.map((g) => (
          // each group collapses on its own: the titles carry counts, so a closed
          // group still tells you what is in it
          <details className="srcgrp mt-[.45rem] first:mt-[.15rem]" key={g.title} open>
            <summary className="mb-[.2rem] flex cursor-pointer list-none items-center gap-[.3rem] hover:text-accent">
              <span className="srch m-0 text-[.66rem] font-bold uppercase tracking-[.09em] text-muted">{g.title}</span>
            </summary>
            <ul className="m-0 list-disc pl-[1.1rem]">
              {g.items.map((it, i) => (
                <li className="my-[.12rem] text-[.78rem]" key={i}>{it}</li>
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
    <div className="states mt-[.6rem] grid grid-cols-[repeat(auto-fit,minmax(17rem,1fr))] gap-x-[.9rem] gap-y-[.35rem] text-[.73rem] text-muted">
      <div className="st flex items-start gap-[.45rem] leading-[1.4]">
        <span className="sw plain mt-[.05rem] h-[1.15rem] w-[1.15rem] shrink-0 rounded-[5px] border border-line bg-card" />
        <div>
          <b className="font-semibold text-fg">Solid</b> — a real standalone message in the mailbox. The caret on its header
          opens the ids it was found under, its Gmail message&nbsp;id among them.
        </div>
      </div>
      <div className="st flex items-start gap-[.45rem] leading-[1.4]">
        <span className="sw dash mt-[.05rem] h-[1.15rem] w-[1.15rem] shrink-0 rounded-[5px] border border-dashed border-[color-mix(in_srgb,var(--muted)_55%,transparent)] bg-dash" />
        <div>
          <b className="font-semibold text-fg">Dashed</b> — reconstructed from quoted text inside a later email; no message of
          its own. Its header names the email it came out of, and its timestamp is the one in
          the quoted header.
        </div>
      </div>
      <div className="st flex items-start gap-[.45rem] leading-[1.4]">
        <span className="sw mine mt-[.05rem] h-[1.15rem] w-[1.15rem] shrink-0 rounded-[5px] border border-org-3 bg-mine" />
        <div>
          <b className="font-semibold text-fg">Tinted</b> — sent by you.
        </div>
      </div>
      <div className="st flex items-start gap-[.45rem] leading-[1.4]">
        <span className="sw clipsw grid h-[1.15rem] w-[1.15rem] shrink-0 place-items-center border-0 text-[.8rem]">&#128206;</span>
        <div>
          <b className="font-semibold text-fg">Attachment</b> — links through to that message in Gmail. Only detectable on real
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
      <li className="my-[.12rem] text-[.78rem]" key={r.id}>
        <a className="xref text-inherit underline decoration-accent underline-offset-2 hover:text-accent" href={`#${r.id}`}>
          <b>{r.entry.kind === "note" ? r.entry.label : r.entry.sender}</b>,{" "}
          {[r.entry.date, r.entry.time].filter(Boolean).join(" ")}
        </a>
        {r.entry.source ? <span className="note text-[.92em] text-muted"> {"—"} {r.entry.source}</span> : null}
      </li>
    ));

  if (!fresh.length && !revised.length) {
    return (
      <details className="pan mt-[.7rem] rounded-[9px] border border-line bg-card" open>
        <summary className="list-none cursor-pointer px-[.7rem] py-[.4rem] text-[.72rem] font-bold uppercase tracking-[.08em] text-muted hover:text-accent">Since last run</summary>
        <div className="pbody border-t border-line px-[.7rem] pt-[.1rem] pb-[.6rem]">
          <div className="srcgrp mt-[.45rem] first:mt-[.15rem]">
            <ul className="m-0 list-disc pl-[1.1rem]">
              <li className="my-[.12rem] text-[.78rem]">Nothing new. Every entry on this page was already present in {prevLabel}.</li>
            </ul>
          </div>
        </div>
      </details>
    );
  }

  return (
    <details className="pan mt-[.7rem] rounded-[9px] border border-line bg-card" open>
      <summary className="list-none cursor-pointer px-[.7rem] py-[.4rem] text-[.72rem] font-bold uppercase tracking-[.08em] text-muted hover:text-accent">
        Since last run {"—"} {fresh.length} new, {revised.length} revised
      </summary>
      <div className="pbody border-t border-line px-[.7rem] pt-[.1rem] pb-[.6rem]">
        {fresh.length ? (
          <div className="srcgrp mt-[.45rem] first:mt-[.15rem]">
            <div className="srch mb-[.2rem] text-[.66rem] font-bold uppercase tracking-[.09em] text-muted">
              New since {prevLabel} ({fresh.length})
            </div>
            <ul className="m-0 list-disc pl-[1.1rem]">{list(fresh)}</ul>
          </div>
        ) : null}
        {revised.length ? (
          <div className="srcgrp mt-[.45rem] first:mt-[.15rem]">
            <div className="srch mb-[.2rem] text-[.66rem] font-bold uppercase tracking-[.09em] text-muted">Revised ({revised.length})</div>
            <ul className="m-0 list-disc pl-[1.1rem]">{list(revised)}</ul>
          </div>
        ) : null}
      </div>
    </details>
  );
}
