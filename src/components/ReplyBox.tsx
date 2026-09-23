import { Fragment, useEffect, useMemo, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { $api, type CorpusEntry, type SendResponse } from "../lib/api";
import { dismissToast, pushToast } from "../lib/toasts";
import { addressesOf, usePersonAddresses, withAddress } from "../lib/who";
import { addressKey, AddressField, type Address } from "./AddressField";
import { refusal, staleAfterMail, SAID_MS } from "./MailVerbs";

/**
 * The reply box: the one thing this pane can say back.
 *
 * A reader who has just read a thread could already archive it, trash it, file it
 * away and mark it read; what they could not do was answer it. This is that, and
 * it is deliberately the smallest version of it that is still worth having: one
 * message, one field, plain text, and the message being answered quoted under the
 * reader's words.
 *
 * **It answers everyone the message was addressed to, and who else it reaches is the
 * reader's to name — that widening is deliberate, and this is where it is stated.** The
 * server takes the answered message's own headers — its sender, and its original To and
 * Cc — and hands them back with the plan as the audience the reply starts from; the two
 * address fields on the preview are where the reader edits that audience, and an address
 * the message did not carry can be typed into them. So a reply can reach somebody this
 * thread has never seen, which the box before this could not do and the mailbox before
 * this refused.
 *
 * What still bounds a send is not the audience but the surface. The server answers only
 * on its loopback bind, with no authentication — a request is whoever can reach the port
 * — and it sends no mail at all unless it was started with -send-mail. So the grant is
 * what says whether this is a surface for answering your own correspondence or an
 * outbound channel to anywhere: a host that leaves -send-mail on is a host where whoever
 * can reach the port can name any address they can type and send mail as the reader to
 * it. A host that must not send to arbitrary addresses is a host that should not grant
 * it. The addresses are checked for shape and for being named once, and nothing else —
 * whether a domain exists is the mailbox's answer, not this screen's.
 *
 * **Which message it answers is the reader's, and it is the whole of that choice.**
 * By itself the box answers the newest message in this thread the mailbox holds —
 * the last thing said that can be answered — and the one way to name another is the
 * press in a message's own receipt (see AnswerPress): a message on this page, one the
 * corpus holds a mailbox copy of. So the reader chooses among the messages they are
 * reading and nothing else, which is what makes the target a choice rather than a
 * guess — and it is a choice about *whom they are answering*, which is not the same
 * question as who the answer reaches (the fields on the plan are that one).
 *
 * **The quote is the server's, not this component's.** What the reader types is
 * their words alone; the message being answered is quoted and attributed by the
 * code that sends it (internal/spec's ReplyBody, internal/corpus's ReplyTarget), so
 * the text under the reply is the same text whichever client asked for it —
 * including a client that is not this one. A quote composed here could be forged,
 * would be a second implementation of the same thing, and would make the preview
 * a drawing of the message rather than the message.
 *
 * **Nothing is sent on the first press.** `preview` is a read: the server prepares
 * the reply and answers with the plan — who it goes to, what it is called, and the
 * whole body with the quote in it — and the reader is shown exactly that. Only
 * `send this reply` writes to the mailbox, and it writes the same body the reader
 * just read. A reply cannot be recalled, so the last thing before one goes out is
 * a screen with the message on it rather than a button that promised.
 *
 * What the preview shows is deliberately the *whole* object rather than a summary:
 * a quote of a long message is the bulk of what will be sent, and a reader who is
 * going to send a signature, a footer or half of the previous message along with
 * their sentence should be able to see that before it leaves — which is also why
 * the plan names the cc: who else is on the answer is the one part of it a reader
 * can still change at that point, and the whole reason the plan is a control rather
 * than a receipt. What the box does not offer is offered nowhere: no attachments, no
 * drafts, no send-later. Who the reply reaches is a field — the two address fields on
 * the preview, and the component they are (see AddressField) — which is the one thing
 * on this screen a reader can name rather than only arrange.
 *
 * **The line above the field names that audience, by name.** "everyone the message
 * was addressed to" was the box asking the reader to trust that it knew; the names
 * are the corpus's own rows for the people on that message (see `audience` below),
 * so a reader can see that the reply is going to the same six colleagues the thread
 * has, and see the one they meant to leave off before they press anything. It is the
 * same audience the mailbox will use and not a second opinion about it: the names
 * come from the same headers, read by the corpus instead of by docket, and the preview
 * prints the addresses the mailbox itself resolved — that, and not this line, is the
 * last word before a send, and it is also where the reader arranges who the reply
 * carries (see the address fields on the plan below).
 *
 * **The two ticks are both subtractions, and both are on.** The reply is one message
 * in two renderings — the words as text, and the same words as HTML with the quote
 * inside a blockquote a client folds — and the second tick takes the second
 * rendering off it for a correspondent who wants the text. That is a choice about
 * the form and not about the message: the words, the quote and the audience are the
 * same either way, so a reader who sends plain text has sent exactly what the
 * preview showed them, minus markup they never wrote.
 *
 * **And the plan's audience is a pair of address fields, which is the third choice on
 * this screen and the only one that can add.** The plan carries the addresses the
 * mailbox resolved (see SendResponse's toRecipients/ccRecipients) rather than only the
 * headers it will render them into, so what the preview draws is the audience itself as
 * a control: one field per list, each holding the addresses in it as chips — take one
 * off the reply, press it to send it in the other list, or type into either field to
 * name one (see AddressField). It is the same kind of move as the reply-all tick, made
 * finer and made wider: the reply no longer reaches only the people the message did.
 *
 * **Two addresses are refused however they are typed or picked: the reader's own, and
 * one that is already on the reply.** One recipient is one address in one list, and a
 * reply is not sent to the person writing it — the corpus knows which address that is
 * from the same settings read the cc line above uses, and either refusal is said in
 * words beside the field rather than by silently dropping what was typed. Everything
 * else is accepted, including an address the corpus has never seen, which is the whole
 * point of a field that can be typed into. What the second press sends is the audience
 * the fields hold, named address by address in the request (see SendRequest's to/cc),
 * so the message that leaves is the one the reader arranged; while to is empty the
 * press is disabled, because a reply with nobody on it is not a reply.
 *
 * A refusal says what it was: a host started without -send-mail cannot answer mail
 * at all, and the second step says so in words rather than being a button that
 * does nothing. Neither failure claims the other's outcome — a mailbox that would
 * not prepare the reply sent nothing, and a mailbox that did not answer the send
 * may have sent it, which the server's own message says.
 *
 * **The refusal is drawn here and the sent account is not**, which is the
 * difference between something to act on and something that is over. A failure
 * leaves the plan on screen with the reader's words in it and says what went
 * wrong beside them; what a send *did* goes to the shell's corner (see Toasts),
 * because a sentence about a message that has gone is not state of the box — it
 * must not take a row from the trail the reader is reading, and it outlives this
 * box the moment somebody clicks another thread.
 */
/**
 * Everyone a reply-all would reach besides the person who wrote the message: the
 * names recorded on it in To or Cc, minus the reader's own person, in the order
 * the message carried them.
 *
 * Read off the entry's participants rather than by taking the header text apart
 * here, because those rows are the corpus's own answer about who is on a message —
 * the same rows the participants panel and the "to" line under the bubble are drawn
 * from — and a split of the display line on commas would be a second, worse reading
 * of it. Minus the reader for the reason the mailbox leaves them out of its cc: a
 * reply is not addressed back to the person writing it, and a line naming the reader
 * as somebody this goes to would be the box asking them to check a list it had got
 * wrong. Names are deduplicated as they are printed: two person rows for one
 * colleague are one name on this line.
 *
 * This is the audience the mailbox will use, not a rival to it — the same headers,
 * read by the corpus instead of by docket, and the preview prints the addresses the
 * mailbox itself resolved. `me` is the reader's person id as /v1/settings gives it;
 * with none named, nothing is subtracted, which is the honest reading of a reader
 * who has not said who they are.
 */
export function audience(entry: CorpusEntry, me: number | undefined): string[] {
  const names: string[] = [];
  for (const p of entry.participants ?? []) {
    if (p.role === "from" || (me !== undefined && p.personId === me)) continue;
    if (!names.includes(p.name)) names.push(p.name);
  }
  return names;
}

/** A list as a sentence says it: "Ada", "Ada and Bo", "Ada, Bo and Cy" — and each
 *  name is its own element, because each one carries the addresses behind it as a
 *  hover title. The separators are between the names rather than inside them: a
 *  title on "Ada and Bo" would answer a hover about one person with two. */
function names(names: string[], title: (name: string) => string) {
  return names.map((name, i) => (
    <Fragment key={name}>
      {i === 0 ? "" : i === names.length - 1 ? " and " : ", "}
      <span title={title(name)}>{name}</span>
    </Fragment>
  ));
}

/**
 * The press that points the box below at the message above it: "reply all", in
 * the receipt of a bubble's header, beside the control that copies the message and
 * the one that switches to the sender's own rendering.
 *
 * **It is what makes the box's target a choice, and it is the whole of that
 * choice.** Before it, the box answered the newest message the mailbox held and
 * there was nothing on the screen that could name a different one; now a reader
 * who wants to answer the message that asked them something — rather than the
 * latest line in the thread, which often answers nothing — can say so from the
 * message itself. What it does to the audience is the rest of its name: the box's
 * reply-all tick goes on, because a press that says "reply all" and leaves a
 * reply to the sender alone would be a button lying about what it had done.
 *
 * **It chooses the message rather than the audience.** The message is a message on
 * this page, one the corpus holds — a press cannot name a correspondent the reader
 * has not been reading — and the audience of the answer is still assembled from that
 * message's own headers by the mailbox. What a reader gains here is an older message,
 * not a new recipient: who else the reply ends up reaching is decided further down,
 * in the fields on the plan (see ReplyBox).
 *
 * Drawn only where the caller has both things: a mailbox copy of the message to
 * thread an answer onto (see gmailIdOf), and a box to point at it. A message
 * recovered from somebody's quote has no mailbox copy, and a built page has no
 * box — in both the press is left off rather than offered and refused.
 *
 * Pressed is which message the box is answering now, which is state of the pane
 * rather than of this message: the newest answerable message wears it until a
 * reader presses something else, so the one piece of the box's state a reader
 * cannot see from here — which message, out of a thread of thirty — is legible in
 * the thread itself.
 */
export function AnswerPress({
  extId,
  pressed,
  onPress,
}: {
  /** The message this press names, as the box takes it. */
  extId: string;
  /** Whether the box is already answering this message. */
  pressed: boolean;
  onPress: (extId: string) => void;
}) {
  const label = pressed
    ? "This is the message the box below is answering"
    : "Reply all to this message, in the box at the bottom of the thread";
  return (
    <button
      type="button"
      className="replyall"
      aria-pressed={pressed}
      title={label}
      aria-label={label}
      onClick={() => onPress(extId)}
    >
      {/* The reply arrow, twice: the mark the line under a bubble already wears
          (↩, see ReplyLink), drawn as a stroke glyph. Two heads, and the tail
          starts at the INNER head's point — from there it runs out past both heads
          and curves down clear of them. The first drawing of this ran the tail from
          the outer head, so it crossed the inner head's lower arm on its way out
          and the pair read as one arrow with a scratch through it; a reply-all is
          two arrows, and nothing in it should cross anything. */}
      <svg viewBox="0 0 16 16" width="18" height="18" aria-hidden="true">
        <path d="M5.2 4.6 1.8 8l3.4 3.4" fill="none" stroke="currentColor"
          strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
        <path d="M9 4.6 5.6 8l3.4 3.4" fill="none" stroke="currentColor"
          strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
        <path d="M5.6 8h5.6a2.8 2.8 0 0 1 2.8 2.8v1" fill="none" stroke="currentColor"
          strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
    </button>
  );
}

export function ReplyBox({
  thread,
  answer,
  answers,
  words,
  all,
  onAll,
  newest,
  aimed,
}: {
  /** The thread being read, which is what is re-read once the answer is in it. */
  thread: { rootExtId: string };
  /** The message being answered: by default the newest entry of this thread the
   *  mailbox holds, and the message a press on a header's answer control names
   *  when the reader wants an older one. See AnswerPress below. */
  answer: CorpusEntry;
  /** Where that message's own bubble is on the page — the element id the pane's
   *  anchor map gives it (see ThreadMessages' anchor), so the header below can
   *  name the same element the reply link under that bubble names. Passed in
   *  rather than recomputed here: a second id map inside this box would be a
   *  second answer to "which element is this message", and the reply link, the
   *  bubble's own id and this header must all give the same one. */
  answers: string;
  /** How that message's own bubble names it — the name and clock its head wears
   *  (see ThreadMessages' stamp words). Passed in rather than written again here:
   *  the box says "replying to Lena Whitfield, Mon 2 Mar 2026 09:15", and a reader
   *  looking at the same message in the same pane must not be told a different
   *  clock by the reply box than by the bubble above it. */
  words: { who: string; whoTitle?: string; when: string };
  /** Whether the answer goes to everyone the answered message was addressed to or
   *  to its sender alone. Held by the pane rather than here, because a header's
   *  answer control is a press about the audience as well as about the message:
   *  "reply all" is what it says, so it is what it must turn on. */
  all: boolean;
  onAll: (all: boolean) => void;
  /** Whether the message being answered is the newest one the pane could answer.
   *  The line above the field says which message this is, and "the newest here" is
   *  only true of one of them. */
  newest: boolean;
  /** How many times a header's answer control has been pressed, across the life
   *  of this box — a count rather than a flag, because pressing the control on the
   *  message already answered is still a press and must still bring the box up. */
  aimed: number;
}) {
  const queryClient = useQueryClient();
  // What the reader has written, in their own words: plain text, and no quote of
  // the message being answered — that is added on the way out, so the reader is
  // never editing around text they did not write.
  const [own, setOwn] = useState("");
  // Whether the reply carries the HTML part beside the plain text. On by default,
  // for the same reason and with the same shape as the tick above: it is what this
  // box has always sent, so the tick takes the second rendering off a reply rather
  // than asking the reader to add it. The words are the same either way — the HTML
  // is the same message marked up — so what the reader is choosing is not what the
  // reply says but whether the correspondent's client gets a quote it can fold and
  // paragraphs that are paragraphs.
  const [html, setHtml] = useState(true);
  // The plan the preview came back with, while the reader is looking at it.
  const [plan, setPlan] = useState<SendResponse | null>(null);
  // The plan's audience as the reader arranges it, in the two lists it is carried
  // in — the address field's value for each. Held beside the plan rather than
  // derived from it because it is the one part of the plan the reader edits, and set
  // with it so the two are never a plan and a stale field. **null** is not an empty
  // list: it is a plan that carried no recipient addresses at all (a fixture or an
  // older server — see the fallback below), where there is nothing to seed a field
  // with and the preview prints the headers it was given instead.
  const [to, setTo] = useState<Address[] | null>(null);
  const [cc, setCc] = useState<Address[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  // The box's own element, which the aim below brings to the reader. A ref rather
  // than a document query, because this is one box among any the shell has drawn.
  const host = useRef<HTMLDivElement | null>(null);

  // A plan is a claim about the message the box was answering when it was made:
  // its recipients, its subject, and a quote of that message in the body the
  // reader was shown. Retarget the box and the claim is about a message it no
  // longer names — and the second press would recompose the body for the new
  // target while the reader checked the old one (see ship). So the plan goes, and
  // the reader's words stay: the words are theirs, and a preview is a step they
  // take again rather than something to be carried across.
  useEffect(() => {
    setPlan(null);
    setTo(null);
    setCc(null);
  }, [answer.extId]);

  // A press on a message's own answer control is an intent to write, and the box
  // sits at the bottom of a thread the message may be screens away from — so the
  // box comes up to the reader instead of the reader hunting for it. The field
  // takes the cursor where it is already drawn; while a plan is up there is no
  // field to put one in, and the reader is being shown the message they were about
  // to send rather than being asked to write another.
  useEffect(() => {
    if (!aimed) return;
    host.current?.scrollIntoView?.({ block: "center" });
    host.current?.querySelector<HTMLTextAreaElement>("textarea")?.focus();
  }, [aimed]);

  // What the last send here has to say is drawn in the shell's corner (see
  // Toasts), and what this keeps is the id of its own notification, for the
  // reason the pane keeps its own: the box is not remounted between threads, only
  // its contents change, so an account of a reply is a claim about a trail the
  // reader may have left by the time they read it. Taken down by hand when the
  // thread changes rather than left to its clock, and the next send takes down the
  // one before it — a reply cannot be recalled, so the account of it must not be
  // either, and it must not be one of two.
  const said = useRef<number | null>(null);
  const say = (text: string) => {
    if (said.current !== null) dismissToast(said.current);
    said.current = pushToast(text, "note", SAID_MS);
  };

  useEffect(() => {
    if (said.current !== null) dismissToast(said.current);
    said.current = null;
  }, [thread.rootExtId]);

  const send = $api.useMutation("post", "/v1/send");

  // The two lists the plan is drawn from, and whether the plan carried any at all.
  // A plan that carried only the rendered headers has nothing to seed a field with,
  // and the preview falls back to printing what it has rather than inventing an
  // audience to draw (see below).
  const addressed = to !== null && cc !== null;

  // Where an address moves to when its chip's press is taken: the same address, put
  // in the other list. Neither press can lose an address — the two lists are the
  // whole of the audience the reply carries — so this is one write of both, and the
  // address keeps whatever name it had.
  const move = (from: "to" | "cc", who: Address) => {
    const add = (list: Address[]) =>
      list.some((a) => addressKey(a.address) === addressKey(who.address)) ? list : [...list, who];
    if (from === "to") setCc(add(cc ?? []));
    else setTo(add(to ?? []));
  };

  // The plan and its audience go together: a plan is not a thing to arrange once the
  // reader is editing again, and a message that has gone has no audience left here.
  const unplan = () => {
    setPlan(null);
    setTo(null);
    setCc(null);
  };

  // Who the reply would reach besides the sender, named above the field. The
  // settings read is what knows which of the people on the message is the reader;
  // it is a read of the local store, and the pane's neighbours already keep it
  // warm.
  const settings = $api.useQuery("get", "/v1/settings", {});
  const others = audience(answer, settings.data?.mePersonId);

  // The addresses behind those names. The message carries a person id and a name for
  // each of them and no address (see castOfEntries in Participants.tsx, and lib/who),
  // because nothing in a thread read records where a recipient's copy was sent — so
  // the corpus's identity graph is asked, and a name whose person has an address
  // gets it as a hover title. Every row for a printed name contributes, since the
  // line prints one name for what may be two rows of the same person.
  const people = usePersonAddresses();
  const addressOf = (name: string) =>
    withAddress(
      name,
      (answer.participants ?? []).filter((p) => p.name === name).flatMap((p) => people.get(p.personId) ?? []),
    );

  // Everything the fields may offer, in the order they would rather offer it — the
  // reader picks from the top of a list, so the likeliest address goes there. Three
  // sources: the people on the message being answered, whose addresses the corpus
  // holds under the person ids the message carries; the recipients the plan
  // resolved, which bring the names the message itself gave; and then everyone else
  // the corpus knows, so answering somebody who was not on this thread is a pick
  // rather than a retyping. Folded by address, first source wins, because a person
  // is one address here and two rows for them would be two chips.
  const roster = $api.useQuery("get", "/v1/people", {});
  const suggestions = useMemo(() => {
    const out: Address[] = [];
    const seen = new Set<string>();
    const offer = (who: Address) => {
      const key = addressKey(who.address);
      if (!key || seen.has(key)) return;
      seen.add(key);
      out.push(who);
    };
    for (const p of answer.participants ?? [])
      for (const address of people.get(p.personId) ?? []) offer({ name: p.name, address });
    for (const r of [...(plan?.toRecipients ?? []), ...(plan?.ccRecipients ?? [])])
      offer({ name: r.name, address: r.address });
    for (const p of roster.data?.people ?? [])
      for (const address of addressesOf(p.identities)) offer({ name: p.displayName, address });
    return out;
  }, [answer, people, plan, roster.data]);

  // The reader's own addresses, which a reply is not sent to. Read the way the cc
  // line's titles are read: the settings say which person the reader is, and the
  // corpus's identity graph says what addresses that person has. Nothing else is
  // guessed at — a second mailbox or a forwarding alias the corpus has never seen
  // would be a guess, and a wrong one refuses an address for no reason.
  const mine = useMemo(
    () => (settings.data?.mePersonId === undefined ? [] : people.get(settings.data.mePersonId) ?? []),
    [people, settings.data],
  );

  // The first press: a read. The server composes the reply and answers with the
  // plan, and nothing is written to the mailbox — so a press that fails here, from
  // an entry the mailbox does not hold or a host without the grant, has cost the
  // reader nothing and left nothing behind.
  async function review() {
    setError(null);
    setBusy(true);
    try {
      const res = await send.mutateAsync({
        body: { entry: answer.extId, body: own, all, html, confirm: false },
      });
      setPlan(res);
      // The audience comes with the plan, because the plan is where it is known:
      // every address the mailbox resolved, in the lists it put them in. A plan
      // that carried no addresses at all — a fixture, or a server from before the
      // plan named them — leaves the fields unseeded, and the preview prints the
      // headers it was given instead (see below).
      const lists = res.toRecipients || res.ccRecipients;
      setTo(lists ? (res.toRecipients ?? []) : null);
      setCc(lists ? (res.ccRecipients ?? []) : null);
    } catch (e) {
      // Said on the pane rather than only in the console, for the reason the
      // verbs' refusals are: a button that answers with nothing reads as broken,
      // and the disabled case in particular is a thing the reader can act on.
      setError(refusal(e, "-send-mail", "Preparing the reply"));
    }
    setBusy(false);
  }

  // The second press: the send. The body is the one the reader was just shown —
  // `plan.body` is the mailbox's own answer about what a reply says, and it is
  // deliberately not recomposed here, so what goes out is what was read. The
  // audience is likewise the one the fields show, named address by address: the
  // addressing itself is the fields' (see AddressField) and all this does is hand
  // over what they hold.
  //
  // Naming the addresses is what the send does even when the reader changed
  // nothing, because a list that is only ever the message's own audience cannot be
  // said differently from the one that was read — and the whole point of the fields
  // is that the reader may say it differently. Each entry is the bare address: a
  // display name in a header is a formatting question with its own rules, and
  // naming one here would be a second implementation of how a name is written.
  // What the reader typed is what goes, and a name the message already carried is
  // kept by the mailbox (see gmailclient's WithRecipients).
  //
  // Once it has gone, the thread is re-read: the server files the sent message
  // into the corpus in the same request, so the answer appears in this trail as
  // the mailbox's own copy of it — the reply is not an optimistic bubble that
  // might not exist. The lists are stale as well (a thread's newest message, its
  // count, the corpus's own totals all just changed) and are invalidated by the
  // same helper the other mailbox writes use, so "answered" and "archived" leave
  // the app holding the same view of the mailbox.
  async function ship() {
    if (!plan) return;
    setError(null);
    setBusy(true);
    try {
      await send.mutateAsync({
        body: {
          entry: answer.extId,
          body: own,
          all,
          html,
          confirm: true,
          // The audience the reader arranged, as the bare addresses the mailbox
          // resolved — the same set the fields hold, so what goes out is what was
          // previewed. Absent when the plan carried no recipient addresses, which
          // leaves the audience as the mailbox assembled it.
          ...(addressed
            ? {
                to: (to ?? []).map((a) => a.address),
                cc: (cc ?? []).map((a) => a.address),
              }
            : {}),
        },
      });
      unplan();
      setOwn("");
      // Said in the corner rather than here, and in the same words: an answer that
      // has gone out is over, and what the reader is looking at now is the trail
      // it was filed into.
      say(`Answered ${words.who || "the sender"} — sent, and filed in the trail below.`);
    } catch (e) {
      // The plan stays on screen: the reader's words are still theirs, and the
      // server's message says which of the two failures this was.
      setError(refusal(e, "-send-mail", "Sending the reply"));
    }
    setBusy(false);
    staleAfterMail(queryClient);
  }

  return (
    <div className="replybox" ref={host}>
      {/* The header says which message this box answers, and it is pointed at to
          check that claim — so it carries the answered message's element id in
          `data-answers`, which the document's delegated listener rings the named
          bubble for (see behaviour.ts). An attribute rather than an anchor: the
          box sits at the bottom of the thread to be typed in, and a link would
          scroll the reader away from it. The whole line makes the claim, so the
          attribute is on the paragraph rather than on any name inside it. */}
      <p className="replyto" data-answers={answers}>
        Replying to <span title={words.whoTitle ?? words.who}>{words.who || "the sender"}</span>
        {words.when ? `, ${words.when}` : ""}
        {all && others.length ? (
          // A fragment rather than a template string, because each name in the list
          // is an element now: it carries the addresses behind it as a title.
          <>
            , cc {names(others, addressOf)}
          </>
        ) : (
          ", and nobody else on it"
        )}{" "}
        {/* Which message this is, which the reader needs to see because the box no
            longer always answers the one they would guess: a press on an older
            message's own control moves it, and the sentence is where that shows. */}
        {newest
          ? "— the newest message here the mailbox holds."
          : "— the message you pressed reply all on, not the newest one here."}
      </p>
      {error ? (
        <p className="selfail" role="alert">
          {error}
        </p>
      ) : null}
      {plan ? (
        <div className="replyplan">
          <p className="replynote">
            <strong>Nothing has been sent yet.</strong> This is the whole message as
            it will go:{" "}
            {/* Who it goes to is the fields themselves rather than a sentence beside
                a control that restates it: what a reader checks before a send is
                the list that is going, and a line that printed the list a second
                time would be a second answer to it — the reader's arrangement, read
                off two things that could disagree. Both lists are always drawn,
                even when one of them is empty, because a reply that had no cc is
                exactly the reply a reader may want to add one to. */}
            {addressed ? (
              <>
                to{" "}
                <AddressField
                  label="to"
                  moveTo="cc"
                  move={(who) => move("to", who)}
                  value={to ?? []}
                  onChange={setTo}
                  suggestions={suggestions}
                  taken={cc ?? []}
                  mine={mine}
                  disabled={busy}
                />
                {(to ?? []).length ? null : (
                  // The fact rather than the consequence: the line is a claim about the
                  // message as it will go, and the press at the bottom is what refuses.
                  <> (nobody in to)</>
                )}
                , cc{" "}
                <AddressField
                  label="cc"
                  moveTo="to"
                  move={(who) => move("cc", who)}
                  value={cc ?? []}
                  onChange={setCc}
                  suggestions={suggestions}
                  taken={to ?? []}
                  mine={mine}
                  disabled={busy}
                />
              </>
            ) : (
              <>
                to <strong>{plan.to}</strong>
                {plan.cc ? (
                  <>
                    , cc <strong>{plan.cc}</strong>
                  </>
                ) : null}
              </>
            )}
            , as <strong>{plan.subject}</strong>, in <strong>{plan.html ? "text and HTML" : "plain text alone"}</strong>. Your words come first and the message you
            are answering is quoted under them.
          </p>
          {/* The body as it will be sent, in the form it will be sent in. The html
              tick decides whether the reply carries its HTML part, and the server
              hands that part back on the plan — so what is drawn is the rendering
              that is going out rather than the other one: the reader's words as
              paragraphs and the message being answered inside the blockquote a
              client folds. With the tick off there is no HTML half and the text
              part is the whole message, so it is drawn as the lines that will be
              sent, whitespace and all: a quote is line by line, and a reply that
              reflowed on its way out would not be this text.

              Either way the bytes are the server's and are rendered as they are:
              the HTML was composed by spec.ComposeReply, from the same reading of
              the reader's words as the text beside it, and a second pass here —
              sanitising or restyling it — would be a second answer to what is
              being sent. It goes in this stylesheet rather than a shadow root for
              the reason the two are not the same thing: mountOriginal holds a
              sender's own document, with their own stylesheet and their own class
              names to contain (see lib/original). Here there is neither — this is
              the app's own markup, composed by the same package that composes
              every body the pane already draws this way — so the blockquote and
              the paragraphs are meant to read as the app reads mail. */}
          {plan.html ? (
            <div className="replyhtml" dangerouslySetInnerHTML={{ __html: plan.html }} />
          ) : (
            <pre className="replytext">{plan.body}</pre>
          )}
          <div className="opmact replyacts">
            <button
              type="button"
              className="opbtn"
              disabled={busy}
              onClick={unplan}
            >
              keep editing
            </button>
            <button
              type="button"
              className="opbtn opbtn-after"
              disabled={busy || !(to ?? []).length}
              title={
                (to ?? []).length
                  ? "Send the reply, as it reads above."
                  : "A reply needs somebody in to — put an address there first."
              }
              onClick={() => void ship()}
            >
              {busy ? "sending…" : "send this reply"}
            </button>
          </div>
        </div>
      ) : (
        <>
          <textarea
            className="replyinput"
            aria-label="Your reply"
            rows={4}
            value={own}
            disabled={busy}
            onChange={(e) => setOwn(e.target.value)}
          />
          <div className="opmact replyacts">
            {/* The two decisions the box has to make, at the left end of the row
                where a form's choices sit, and the press that acts on them at the
                right where the sending is. Both are ticks and both are on, and both
                are about the *starting* audience rather than the whole of it: each one
                takes something off the reply as the mailbox first assembled it — the
                audience beyond the sender, and the second rendering of the words —
                and what either leaves out can be put back in the address fields on
                the plan, which is also where the audience grows. */}
            <div className="replyopts">
              <label
                className="replytick"
                title={
                  "Answer everyone the message was addressed to, not only whoever wrote it. " +
                  "Who that is comes from the message itself — the fields on the plan are where " +
                  "the reply's audience is edited."
                }
              >
                <input
                  type="checkbox"
                  checked={all}
                  disabled={busy}
                  onChange={(e) => onAll(e.target.checked)}
                />
                reply all
              </label>
              <label
                className="replytick"
                title={
                  "Send the HTML part of the reply — the same words marked up, with the " +
                  "quoted message in a blockquote a client can fold — beside the plain " +
                  "text. Unchecked sends the plain text alone, which says the same thing."
                }
              >
                <input
                  type="checkbox"
                  checked={html}
                  disabled={busy}
                  onChange={(e) => setHtml(e.target.checked)}
                />
                send html
              </label>
            </div>
            <button
              type="button"
              className="opbtn"
              disabled={busy || own.trim() === ""}
              onClick={() => void review()}
            >
              {busy ? "preparing…" : "preview"}
            </button>
          </div>
        </>
      )}
    </div>
  );
}
