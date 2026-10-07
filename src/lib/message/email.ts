import type { CorpusEntry } from "../api/api";
import type { Entry } from "../timeline/spec";
import { stampOf, type StampData } from "../ui/stamp";
import type { Attachment } from "./attachments";
import { senderTitle } from "./who";

/** The email's own parts, as a bubble draws them. */
export type MessageEmail = {
  /** the sender as displayed; absent on a message with no name on it */
  sender?: string;
  /** hover text for the sender, e.g. "Ada Okoye <ada@example.com>"; defaults to the name */
  senderTitle?: string;
  /** keys the local styles switch (see lib/prefs/usePrefs); absent on recovered entries */
  fromEmail?: string;
  org?: string;
  /** as it appeared on the message, e.g. "Bo Halvorsen, cc …"; absent reads "—" */
  to?: string;
  /** the message's own subject; absent on recovered entries and notes */
  subject?: string;
  /** presentation HTML, already sanitised; edges are trimmed here */
  body: string;
  stamp: StampData;
  attachments?: Attachment[];
  /** people @-named in the body, shown above it */
  mentions?: string[];
  /** the corpus's handle for this message, which the fetch button asks for */
  extId?: string;
  /** the reader's own outbound */
  me?: boolean;
  /** reconstructed from quoted text; drawn dashed */
  quoted?: boolean;
};

/** The thread pane doesn't label orgs, so `org` is left off. */
export function emailFromCorpus(e: CorpusEntry): MessageEmail {
  return {
    sender: e.author,
    senderTitle: senderTitle(e),
    fromEmail: e.fromEmail,
    to: e.to,
    subject: e.subject,
    body: e.html ?? "",
    stamp: stampOf(e),
    attachments: e.attachments,
    extId: e.extId,
    me: e.mine,
    quoted: e.quoted,
  };
}

/** `stamp` comes from the derived row, which has placed the entry's zone. */
export function emailFromSpec(
  e: Entry,
  stamp: StampData,
  whoTitle: (name: string) => string,
): MessageEmail {
  return {
    sender: e.sender,
    senderTitle: whoTitle(e.sender ?? ""),
    fromEmail: e.fromEmail,
    org: e.org,
    to: e.to,
    subject: e.subject,
    body: e.body,
    stamp,
    attachments: e.attachments,
    mentions: e.mentions,
    extId: e.extId,
    me: e.me,
    quoted: e.quoted,
  };
}
