import { useMemo } from "react";
import { $api, type CorpusEntry } from "./api";

/**
 * Who is behind a name, said the two ways this app has to say it.
 *
 * A name on its own is not a claim a reader can check: two colleagues are two
 * "Michael"s in a cast of fifteen, and the address is the part of a person the
 * corpus actually holds evidence for. So every name in the UI carries the
 * addresses it answers to, in a hover title — the bubbles have done this from the
 * start (see senderTitle below), and this module is where the rest of the app gets
 * the same string without re-deriving it.
 *
 * Two sources, because the app has two situations and they are not the same
 * question:
 *
 * - A message's own From header answers "which address did THIS mail come from",
 *   and that is what `senderTitle` says, per entry. It is the only source for a
 *   message recovered from inside someone else's quote, which has no header of its
 *   own — and the corpus refuses to lend one there, because an address reached by
 *   matching a sender's *name* is not evidence about who sent this.
 * - The corpus's identity graph (see /v1/people) answers "which addresses does this
 *   PERSON answer to", and that is what the graph read here says, per person. It is
 *   the only source for a name that sent nothing in the thread being read: the
 *   entry's participants carry a person id and a name and no address at all (see
 *   castOfEntries in Participants.tsx), so a recipient, a cc and a name in a reply
 *   box's audience line would otherwise have no address to show.
 *
 * The distinction is why both exist rather than one replacing the other: a bubble
 * is a claim about a message and asks the header; a name in a list is a claim about
 * a person and asks the graph.
 */

/** The addresses in a person's identity list, in the order the corpus gives them
 *  and without the non-address identities it also keeps (a Slack uid, the
 *  display names a mail arrived with, the spelling of a name that is not a name
 *  — see the `display_name:` rows in /v1/people). */
export function addressesOf(identities: readonly string[] | undefined): string[] {
  const out: string[] = [];
  for (const identity of identities ?? []) {
    if (!identity.startsWith("email:")) continue;
    const address = identity.slice("email:".length);
    if (address && !out.includes(address)) out.push(address);
  }
  return out;
}

/** A name and the addresses it answers to, as a hover title: "Ada Okoye
 *  <ada@loomworks.example>", every address when the corpus holds several, and the
 *  name alone when it holds none. The name alone rather than an empty title, so
 *  that a caller which knows nothing about a person says nothing about them. */
export function withAddress(name: string, addresses: readonly string[] | undefined): string {
  return addresses?.length ? `${name} <${addresses.join(", ")}>` : name;
}

/**
 * The names inside a receipt's `to:` line, and the text that line prints for each.
 *
 * The line arrives as one string — "Bo Halvorsen", "Bo Halvorsen, Cy Okafor", "Maia
 * Bryan, cc Ellen Lindqvist, Lena Whitfield" — because the corpus renders it once for
 * every caller (see recipientLine in internal/spec/people.go, which is also where
 * the shape is decided: the names in To, then `cc ` and the names in Cc). Nothing
 * in the string says which person each name is, so a caller that wants to say
 * something *about* a name has to split it and look the names up; this is that
 * split, in one place, because the format belongs to the renderer rather than to
 * any one bubble.
 *
 * `text` is what the line prints, verbatim, including the `cc ` marker: a receipt
 * is the sender's own list and this app does not restate it. `name` is the part
 * worth looking a person up by. A display name that itself contains a comma is
 * split, which costs it its hover and changes nothing on screen — the pieces are
 * printed back with the same separator they were cut at.
 */
export function receiptNames(to: string): { text: string; name: string }[] {
  return to
    .split(", ")
    .map((text) => text.trim())
    .filter((text) => text !== "")
    .map((text) => ({ text, name: text.replace(/^(cc|to|bcc)\s+/i, "") }));
}

/** What hovering the sender says: their name and the address the mail came from,
 *  e.g. "Lena Whitfield <lane@whitfield.example>". The same string a page build
 *  makes for the same entry (see derive.ts's whoTitle), because a reader reading
 *  one thread in two places should be told the same thing about it.
 *
 *  A message recovered from inside someone else's quote has no From header of its
 *  own, so there is no address to hang on the name — and the corpus will not lend
 *  one, because an address reached by matching the sender's name is not evidence
 *  about who sent this. The absence is named instead, with the address that is
 *  real here and labelled as what it is: the quoter's. Silence would read as the
 *  pane failing to fill in what it fills in on every other bubble in the thread.
 *
 *  The page answers a different question in its own source line, and should keep
 *  doing so: "unspooled from msg g-a" is about where the text came from, and is
 *  printed under every bubble whether or not anyone can be named. A hover is
 *  about the person, so it says which person's address this is rather than where
 *  the entry was found. */
export function senderTitle(e: CorpusEntry): string {
  const name = e.author ?? "";
  if (!e.fromEmail) {
    if (!e.fromQuotedBy) return name;
    // Nothing to hang the parenthesis on when the entry has no name either, so
    // it stands alone rather than starting with a space and a bracket.
    const unknown = `address unknown; quoted by ${e.fromQuotedBy}`;
    return name ? `${name} (${unknown})` : unknown;
  }
  if (!name) return e.fromEmail;
  return `${name} <${e.fromEmail}>`;
}

/**
 * The corpus's people, by person id, as their addresses.
 *
 * One read for the whole app: react-query hands every caller the same cached
 * answer under the same key, and the pages that list people already read this
 * route, so the titles cost no request the app was not making. It is a big
 * answer — a few hundred people and a few hundred addresses, about 50 KB on a
 * corpus of four thousand messages — and it is the same answer for the session,
 * which is what makes joining against it in render cheap enough to do.
 *
 * A person with no address is left out rather than mapped to an empty list:
 * `withAddress` is the thing that decides what to say when there is none, and one
 * absent key says that once instead of every caller testing for emptiness.
 */
export function usePersonAddresses(): Map<number, string[]> {
  const answer = $api.useQuery("get", "/v1/people", {});
  return useMemo(() => {
    const out = new Map<number, string[]>();
    for (const person of answer.data?.people ?? []) {
      const addresses = addressesOf(person.identities);
      if (addresses.length) out.set(person.personId, addresses);
    }
    return out;
  }, [answer.data]);
}
