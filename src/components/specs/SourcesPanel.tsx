import type { View } from "../../lib/timeline/derive";
import type { Timeline as Spec } from "../../lib/timeline/spec";
import { msgCount } from "../../lib/message/sources";
import { attHref } from "../../lib/message/attachments";
import { gmailMessageURL } from "../../lib/message/gmailUrl";
import Checkbox from "../ui/Checkbox";
import type { ThreadFilter } from "./Panels";

type Thread = NonNullable<Spec["threads"]>[number];

export default function SourcesPanel({ v, filter }: { v: View; filter?: ThreadFilter }) {
  const s: Spec = v.spec;
  const groups: { title: string; items: React.ReactNode[] }[] = [];

  if (filter) {
    groups.push({
      title: `Chains (${filter.chains.length})`,
      items: filter.chains.map((c) => (
        <label
          className="flex cursor-pointer items-start gap-1.5 hover:text-accent"
          key={c.root}
          data-chain={c.root}
        >
          <Checkbox
            accent="accent"
            className="mt-0.5 flex-none"
            checked={!filter.excluded.has(c.root)}
            onChange={() => filter.onToggle(c.root)}
          />
          <span>
            {c.subject ?? c.opener}
            {c.gmailId ? (
              <>
                {" "}
                <a
                  className="whitespace-nowrap rounded border border-line px-1 text-[.66rem] font-bold uppercase tracking-[.06em] text-muted no-underline hover:border-accent hover:text-accent"
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
              className="whitespace-nowrap rounded border border-line px-1 text-[.66rem] font-bold uppercase tracking-[.06em] text-muted no-underline hover:border-accent hover:text-accent"
              href={`#${c.anchor}`}
              title="Jump to the start of this thread"
              onClick={(e) => e.stopPropagation()}
            >
              start
            </a>
            <span className="text-[.92em] text-muted">
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
            <span className="font-mono text-[.9em]">{text}</span>
            {note ? <span className="text-[.92em] text-muted"> — {note}</span> : null}
          </span>
        );
      }),
    });
  }

  const threads: Thread[] =
    s.threads ??
    [
      ...new Map(
        v.rows
          .filter((r) => r.entry.threadId ?? r.entry.gmailId)
          .map((r) => [r.entry.threadId ?? r.entry.gmailId!, r]),
      ).entries(),
    ].map(([id, r]): Thread => ({ id, subject: r.entry.subject ?? "(thread)" }));
  if (threads.length) {
    groups.push({
      title: `Mail threads (${threads.length})`,
      items: threads.map((t, i) => {
        const meta = [t.count ? msgCount(t.count) : null, t.span, t.note]
          .filter(Boolean)
          .join(" · ");
        const label = t.subject ?? "(thread)";
        return (
          <span key={i}>
            {t.id ? (
              <a
                className="text-inherit underline decoration-accent underline-offset-2 hover:text-accent"
                href={gmailMessageURL(t.id)}
                target="_blank"
                rel="noopener"
              >
                {label}
              </a>
            ) : (
              label
            )}
            {meta ? <span className="text-[.92em] text-muted"> — {meta}</span> : null}
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
            <a
              className="text-inherit underline decoration-accent underline-offset-2 hover:text-accent"
              href={attHref(a)}
              target="_blank"
              rel="noopener"
            >
              <code>{a.name}</code>
            </a>
          ) : (
            <code>{a.name}</code>
          )}
          <span className="text-[.92em] text-muted">
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
    <details className="pan mt-3 rounded-[9px] border border-line bg-card">
      <summary className="list-none cursor-pointer px-3 py-1.5 text-[.72rem] font-bold uppercase tracking-[.08em] text-muted hover:text-accent">
        Sources &amp; provenance
      </summary>
      <div className="border-t border-line px-3 pt-0.5 pb-2">
        {groups.map((g) => (
          <details className="srcgrp mt-2 first:mt-0.5" key={g.title} open>
            <summary className="mb-1 flex cursor-pointer list-none items-center gap-1 hover:text-accent">
              <span className="m-0 text-[.66rem] font-bold uppercase tracking-[.09em] text-muted">
                {g.title}
              </span>
            </summary>
            <ul className="m-0 list-disc pl-4">
              {g.items.map((it, i) => (
                <li className="my-0.5 text-[.78rem]" key={i}>
                  {it}
                </li>
              ))}
            </ul>
          </details>
        ))}
      </div>
    </details>
  );
}
