import type { CorpusEntry } from "../lib/api";
import { initials } from "../lib/derive";
import type { Timeline as Spec } from "../lib/spec";
import { msgCount } from "../lib/sources";

/** A participant as the panel needs it, however the caller supplied them. */
export type Person = NonNullable<Spec["participants"]>[number];

/**
 * What the participants panel reads.
 *
 * Narrower than the page build's `View` on purpose, because the panel has two
 * callers and only one of them has a spec. A built page hands over its whole
 * `View` — it satisfies this as it stands — while the reading pane has no spec at
 * all: it has the thread read, which carries each entry's sender and the people
 * that entry was addressed to, and a cast is derivable from that and nothing
 * else. Declaring the four things the panel actually uses is what lets both hand
 * it over without either inventing the other's data.
 */
export interface ParticipantsView {
  /**
   * The page being built, when there is one: a spec's `participants` is the cast
   * its author recorded, which is the only place recipients who sent nothing are
   * written down. A caller with no spec (the reading pane) leaves this out and
   * passes the cast it derived as `people` instead — or neither, and the panel
   * lists the senders it can see.
   */
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

/**
 * The cast of a thread, out of the entries a thread read carries.
 *
 * The corpus records who was on every entry — the sender first, then the people it
 * was addressed to — so the pane can list the whole conversation's people rather
 * than only the ones who sent something: an inbox thread is mostly one person
 * writing and several reading, and a panel of senders would leave out everyone who
 * was actually asked.
 *
 * One line per person, in the order the thread introduces them, and the role is the
 * strongest way that person appears anywhere in it: someone who was addressed and
 * then replied is a sender, not whichever of the two a later entry happened to
 * record last. The organisation is the one their own mail is under (the entry they
 * sent), because that is the row the colour belongs to; someone who only ever read
 * the thread has no organisation here and takes the unknown slot, which is the
 * truth about what the corpus knows of them.
 */
export function castOfEntries(entries: CorpusEntry[]): Person[] {
  const out: Person[] = [];
  const at = new Map<number, number>();
  const orgs = new Map<number, string>();
  for (const e of entries) {
    for (const p of e.participants ?? []) {
      if (p.role === "from" && e.org && !orgs.has(p.personId)) orgs.set(p.personId, e.org);
    }
  }
  for (const e of entries) {
    for (const p of e.participants ?? []) {
      const i = at.get(p.personId);
      if (i === undefined) {
        at.set(p.personId, out.length);
        out.push({
          name: p.name,
          role: p.role,
          org: orgs.get(p.personId),
          // Only a sender has an address in a thread read: it is the one the mail
          // came from, and nothing records the addresses of the people it was sent
          // to. The panel says so rather than leaving the space blank.
          email: p.role === "from" ? e.fromEmail : undefined,
        });
        continue;
      }
      const held = out[i]!;
      const role = RANK[p.role]! > (held.role === undefined ? 0 : RANK[held.role]!) ? p.role : held.role;
      const email = held.email ?? (p.role === "from" ? e.fromEmail : undefined);
      if (role !== held.role || email !== held.email) out[i] = { ...held, role, email };
    }
  }
  return out;
}

/** How much a role says about a person, when one person holds several across a
 *  thread: sending something is the strongest claim about why they are in it, and
 *  being copied is the weakest. */
const RANK: Record<string, number> = { from: 3, to: 2, cc: 1 };

function Face({ p, v }: { p: Person; v: ParticipantsView }) {
  // The org comes from the participant's own row, not from a message they sent.
  // Five of the fifteen people on the reference trail send nothing, so looking a
  // colour up through the transcript left them on the unknown grey while the
  // heading directly above them named their org and their colleagues in the same
  // group were coloured.
  const slot = v.orgSlot(p.org);
  // reuses the per-avatar CSS rule rather than inlining the image again
  const pic = v.rows.find((r) => r.entry.sender === p.name)?.avatarClass;
  return (
    <div className={`av ${slot}${pic ? ` pic ${pic}` : ""}`}>
      {pic ? null : <span className="ini">{initials(p.name)}</span>}
    </div>
  );
}

/**
 * Who is in the trail, with addresses.
 *
 * `people` is the cast, when the caller knows more than the senders: the reading
 * pane derives its list from the thread read's own participant rows. A page build
 * needs nothing passed — its `v.spec.participants` is the roster its author
 * recorded, including the people who sent nothing, and the panel reads it from the
 * view. With neither, the panel falls back to the senders the `rows` show — a
 * smaller cast, and worth saying rather than pretending: nothing recorded the
 * recipients, so nobody can be listed who was only addressed.
 *
 * Shut unless `open` is asked for. A panel is a heading over the thing it is about
 * (see the callers): a built page is a document read top to bottom, so its roster
 * is part of it and is open; the reading pane is a thread the reader is on, and
 * fifteen names above the first message are fifteen lines between them and it —
 * the summary says how many people there are, which is what a reader passing
 * through wants, and opening it is one press. */
export function ParticipantsPanel({
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
    [...new Map(v.rows.filter((r) => r.entry.sender).map((r) => [r.entry.sender!, r])).values()].map(
      (r): Person => ({ name: r.entry.sender!, org: r.entry.org, email: r.entry.fromEmail }),
    );

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
    <details className="pan people" open={open}>
      <summary>Participants ({cast.length})</summary>
      <div className="pbody">
        <div className="who">
          {groups.map((g) => (
            <div key={g.org || "other"} style={{ display: "contents" }}>
              {/* Tinted like the org label beside a sender's name, which is how
                  the bubble strip decodes without a legend. One heading per org
                  rather than a mark per row: the transcript is 57 bubbles where a
                  strip reads as structure, and this is a dense list of a dozen
                  rows where a dozen strips would read as noise. The avatars
                  already carry the colour per person. */}
              <div className={`ogh ${v.orgSlot(g.org || undefined)}`}>{g.org || "Other"}</div>
              {g.people.map((p, i) => {
                const n = stats.get(p.name)?.n;
                // The note is shown alongside the count, not only in its absence:
                // it says how a person was seen, which is exactly the thing a
                // count of their messages does not tell you.
                const bits = [
                  p.role,
                  n ? msgCount(n) : undefined,
                  p.note,
                ].filter(Boolean);
                // Keyed by position, because a name is not unique: two corpus
                // people can carry one display name, and both are listed rather
                // than one silently winning.
                return (
                  <div className="p1" key={i}>
                    <div className="pd">
                      <div className="pn">
                        <Face p={p} v={v} />
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
