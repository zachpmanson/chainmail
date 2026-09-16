import { $api, type CorpusEntry } from "../lib/api";
import { Failure } from "./ChainPreview";
import { Message, type StampData } from "./Message";

/**
 * A thread in the reading pane, drawn with the transcript's own `Message`.
 *
 * The pane used to read `/v1/chains` and draw its own cards: plain text, a
 * sender, a day. The page built from the same thread drew the spec pipeline's
 * bubbles — the sender's own formatting, the clock with the zone it was stated
 * in, quote markers, attachments — so the same conversation looked like two
 * different products depending on which of them you were looking at. The
 * component is now shared, and the corpus renders the body (`html`) with the
 * same conversion a build uses, so a bubble here is a bubble there.
 *
 * What is drawn but not yet filled in, because the chain read does not carry it:
 * the org (and so the org colour — every bubble here is on the unknown slot) and
 * attachments. Those live in the spec's per-entry pipeline today. The gaps show
 * as gaps rather than as guesses: an unknown org is the stylesheet's unknown
 * colour, which is what the page shows for a sender whose org nothing
 * established.
 */

/** The transcript's clock, written the way the spec writes it ("Mon 2 Jan 2006",
 *  "15:04"), from the instant and the offset the corpus stored.
 *
 *  With no offset the clock is read in UTC under whatever label the source
 *  stated — the same fallback internal/spec/zones.go makes, and for the same
 *  reason: a label the table cannot turn into an offset is the sender's own word
 *  about their clock, and the pair (label, UTC clock) is at least auditable. The
 *  one thing this cannot do is the page's inference, which reads every placement
 *  in the corpus; here a zone the source did not state is simply unknown. */
function stampOf(e: CorpusEntry): StampData {
  const at = new Date(e.ts);
  if (Number.isNaN(at.getTime())) return { date: e.ts, zone: "unknown" };
  const label = (e.tz ?? "").trim();
  const offset = e.tzOffsetMinutes;
  const wall = new Date(at.getTime() + (offset ?? 0) * 60_000);
  const stated = label !== "" || offset !== undefined;
  return {
    date: DATE.format(wall),
    time: TIME.format(wall),
    tz: label !== "" ? label : offset !== undefined ? formatOffset(offset) : "",
    zone: stated ? "stated" : "unknown",
  };
}

/** Fixed locale and timeZone: the instant has already been shifted into the
 *  sender's clock above, so reading it as UTC is what prints that clock. A
 *  locale-dependent formatter would print a different date in a different
 *  browser, which is exactly the kind of difference a transcript may not have. */
const DATE = new Intl.DateTimeFormat("en-GB", {
  weekday: "short", day: "numeric", month: "short", year: "numeric", timeZone: "UTC",
});
const TIME = new Intl.DateTimeFormat("en-GB", {
  hour: "2-digit", minute: "2-digit", hour12: false, timeZone: "UTC",
});

/** Minutes east of UTC as a Date-header zone, e.g. "+0545". */
function formatOffset(mins: number): string {
  const sign = mins < 0 ? "-" : "+";
  const abs = Math.abs(mins);
  return `${sign}${String(Math.floor(abs / 60)).padStart(2, "0")}${String(abs % 60).padStart(2, "0")}`;
}

/** The anchor a bubble gets. The entry's own ext id is the only stable handle
 *  here, and it is not an id an HTML anchor can carry, so the chain's position
 *  names it: the timestamp's self-link only has to land on the message it is
 *  part of. */
const anchor = (i: number) => `entry-${i}`;

/** What hovering the sender says: their name and the address the mail came from,
 *  e.g. "Lane Whittaker <lane@whittaker.example>". The same string a page build
 *  makes for the same entry (see derive.ts's whoTitle), because a reader reading
 *  one thread in two places should be told the same thing about it. */
function senderTitle(e: CorpusEntry): string {
  const name = e.author ?? "";
  if (!e.fromEmail) return name;
  if (!name) return e.fromEmail;
  return `${name} <${e.fromEmail}>`;
}

export function ChainMessages({ chain }: { chain: { rootExtId: string } }) {
  const fetched = $api.useQuery("get", "/v1/chains/{rootExtId}", {
    params: { path: { rootExtId: chain.rootExtId } },
  });

  if (fetched.isError) return <Failure error={fetched.error} />;
  if (fetched.isPending) return <p className="selnote">Loading the chain…</p>;

  const entries = fetched.data?.entries ?? [];
  if (entries.length === 0) return <p className="selnote">No entries to show.</p>;

  return (
    <div className="stream">
      {entries.map((e, i) => (
        <Message
          key={e.extId}
          id={anchor(i)}
          body={e.html ?? ""}
          sender={e.author}
          // Hovering the name (or the avatar) names the person fully: the address
          // the entry came from, as a page build's own title does. A recovered
          // entry has no address of its own, and then the name stands alone
          // rather than borrowing one from the people table — the same rule the
          // page follows, so the two cannot name two addresses for one message.
          senderTitle={senderTitle(e)}
          // No org on a chain read yet, so no colour can be claimed: `o5` is the
          // stylesheet's unknown slot, the same one the page uses for a sender
          // whose org nothing established.
          orgSlot="o5"
          quoted={e.quoted}
          // The recipients the message itself stated, or nothing: a recovered
          // entry has no headers, and the line reads "to —" rather than naming
          // whoever the page guessed. The corpus makes this string with the same
          // function a page build does.
          to={e.to}
          stamp={stampOf(e)}
          // The corpus entry as it arrived, so a bubble that renders wrong can be
          // pasted somewhere and read whole — the same affordance, and the same
          // button, the page offers.
          copyJson={e}
        />
      ))}
    </div>
  );
}
