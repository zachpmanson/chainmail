import Avatar from "./Avatar";
import { msgCount } from "../../lib/message/sources";
import type { Person } from "./Participants";

// --o5 is unreadable as text on the dark background, so the fifth slot stays muted.
const orgText: Record<string, string> = {
  o1: "text-org-1",
  o2: "text-org-2",
  o3: "text-org-3",
  o4: "text-org-4",
};

/** Narrower than `View` because the reading pane has no spec, only a thread read. */
export interface ParticipantsView {
  /** A spec's `participants` is the only record of recipients who sent nothing. */
  spec?: { participants?: Person[] };
  /** One per message: whose it was, and the avatar that person's face was given. */
  rows: {
    entry: { sender?: string; org?: string; fromEmail?: string };
    avatarClass?: string;
  }[];
  /** the slot class for an org, so a row here matches the sender's own bubbles */
  orgSlot: (org?: string) => string;
  /** name -> "Name <address>" for the hover title, address omitted when unknown */
  whoTitle: (name: string) => string;
}

/** Cast from `people`, else the spec's roster, else only the senders in `rows`. */
export default function ParticipantsPanel({
  v,
  people,
  open,
}: {
  v: ParticipantsView;
  people?: Person[];
  open?: boolean;
}) {
  const cast: Person[] =
    people ??
    v.spec?.participants ??
    [
      ...new Map(v.rows.filter((r) => r.entry.sender).map((r) => [r.entry.sender!, r])).values(),
    ].map((r): Person => ({ name: r.entry.sender!, org: r.entry.org, email: r.entry.fromEmail }));

  const stats = new Map<string, { n: number }>();
  for (const r of v.rows) {
    if (!r.entry.sender) continue;
    stats.set(r.entry.sender, { n: (stats.get(r.entry.sender)?.n ?? 0) + 1 });
  }

  const groups: { org: string; people: Person[] }[] = [];
  for (const p of cast) {
    const org = p.org ?? "";
    const g = groups.find((x) => x.org === org);
    if (g) g.people.push(p);
    else groups.push({ org, people: [p] });
  }

  return (
    <details className="pan people mt-3 rounded-lg border border-line bg-card" open={open}>
      <summary className="list-none cursor-pointer px-3 py-1.5 text-xs font-bold uppercase tracking-[.08em] text-muted hover:text-accent">
        Participants ({cast.length})
      </summary>
      <div className="border-t border-line px-3 pt-0.5 pb-2">
        <div className="grid grid-cols-[repeat(auto-fill,minmax(15.5rem,1fr))] gap-x-4 gap-y-1">
          {groups.map((g) => (
            <div key={g.org || "other"} className="contents">
              <div
                className={`${orgText[v.orgSlot(g.org || undefined)] ?? "text-muted"} col-span-full mt-2 mb-px text-2xs font-bold uppercase tracking-[.09em] first:mt-0.5`}
              >
                {g.org || "Other"}
              </div>
              {g.people.map((p, i) => {
                const n = stats.get(p.name)?.n;
                const bits = [p.role, n ? msgCount(n) : undefined, p.note].filter(Boolean);
                // Keyed by position: two corpus people can share a display name.
                return (
                  <div className="min-w-0 py-1" key={i}>
                    <div className="min-w-0 leading-snug">
                      <div className="flex items-center gap-1.5 text-xs font-semibold">
                        <Avatar
                          name={p.name}
                          orgSlot={v.orgSlot(p.org)}
                          pic={v.rows.find((r) => r.entry.sender === p.name)?.avatarClass}
                          size="participant"
                        />
                        <span className="min-w-0 wrap-anywhere" title={v.whoTitle(p.name)}>
                          {p.name}
                        </span>
                      </div>
                      {p.email ? (
                        <a
                          className="font-mono text-2xs text-muted no-underline wrap-anywhere hover:text-accent hover:underline"
                          href={`mailto:${p.email}`}
                        >
                          {p.email}
                        </a>
                      ) : (
                        <span className="text-2xs text-muted">address not in the trail</span>
                      )}
                      <div className="text-2xs text-muted">{bits.join(" · ")}</div>
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
