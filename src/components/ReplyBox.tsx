import { useEffect, useMemo, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { $api, type CorpusEntry, type SendResponse } from "../lib/api";
import { dismissToast, pushToast } from "../lib/toasts";
import { addressesOf, usePersonAddresses } from "../lib/who";
import { addressKey, AddressField, type Address } from "./AddressField";
import { refusal, staleAfterMail, SAID_MS } from "./MailVerbs";

/**
 * The reply box: the one thing this pane can say back.
 *
 * A reader who has just read a thread could already archive it, trash it, file it
 * away and mark it read; what they could not do was answer it. This is that, and
 * it is deliberately kept small: one line of To and Cc autocomplete plus a link to
 * the message being answered, the reply text area below it, and a preview of what sends.
 *
 * **It answers everyone the message was addressed to, and who else it reaches is the
 * reader's to name — that widening is deliberate, and this is where it is stated.** The
 * server starts from the answered message's own audience. The two address fields on the
 * compose screen let the reader edit it before preview, and accept an address the message
 * did not carry, so a reply can reach somebody this thread has never seen.
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
 * guess — and it is a choice about *which message they are answering*, separate from
 * the audience they arrange in the compose fields.
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
 * The preview shows the whole message, quote and all, so the reader can check exactly
 * what will go out. Its recipient line is the mailbox's authoritative answer; changes
 * happen in the compose fields, then the message is previewed again. The box offers no
 * attachments, drafts or send-later.
 *
 * **The two ticks are both subtractions, and both are on.** The reply is one message
 * in two renderings — the words as text, and the same words as HTML with the quote
 * inside a blockquote a client folds — and the second tick takes the second
 * rendering off it for a correspondent who wants the text. That is a choice about
 * the form and not about the message: the words, the quote and the audience are the
 * same either way, so a reader who sends plain text has sent exactly what the
 * preview showed them, minus markup they never wrote.
 *
 * **The compose screen has a To and a Cc address field.** Each holds the recipients
 * as chips, lets the reader remove an address, and offers autocomplete or direct
 * typing. A list the reader leaves untouched stays under the mailbox's default, so its
 * Reply-To and account-alias handling remains authoritative; the preview then displays
 * the exact resulting audience before anything is sent.
 *
 * **Two addresses are refused however they are typed or picked: the reader's own, and
 * one that is already on the reply.** One recipient is one address in one list, and a
 * reply is not sent to the person writing it — the corpus knows which address that is
 * from the reader setting and identity graph, and either refusal is said in words
 * beside the field rather than by silently dropping what was typed. Everything
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
 * not a new recipient: who else the reply ends up reaching is decided in the To/Cc
 * fields on the compose screen (see ReplyBox).
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

export function ReplyBox({ thread, answer, answerAnchor, words, all, onAll, aimed }: {
  thread: { rootExtId: string };
  answer: CorpusEntry;
  answerAnchor: string;
  words: { who: string; whoTitle?: string; when: string };
  all: boolean;
  onAll: (all: boolean) => void;
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
  // Recipients are arranged on the compose screen, before the message preview.
  // Each list has its own touched bit: leaving one alone lets the mailbox keep its
  // authoritative default (notably Reply-To), while changing the other does not
  // accidentally replace it with a guessed address from the corpus.
  const [to, setTo] = useState<Address[]>([]);
  const [cc, setCc] = useState<Address[]>([]);
  const [toTouched, setToTouched] = useState(false);
  const [ccTouched, setCcTouched] = useState(false);
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
    setTo([]);
    setCc([]);
    setToTouched(false);
    setCcTouched(false);
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

  const changeTo = (next: Address[]) => {
    setToTouched(true);
    setTo(next);
  };
  const changeCc = (next: Address[]) => {
    setCcTouched(true);
    setCc(next);
  };

  // Going back from the checked message returns to composition with the arranged
  // recipients intact. The plan itself is only a preview and is safe to discard.
  const unplan = () => setPlan(null);

  const settings = $api.useQuery("get", "/v1/settings", {});
  const people = usePersonAddresses();
  const roster = $api.useQuery("get", "/v1/people", {});

  // The reader's own addresses, which must not appear in the suggested reply-all
  // audience. The identity graph is the corpus's answer about mailbox aliases.
  const mine = useMemo(
    () => (settings.data?.mePersonId === undefined ? [] : people.get(settings.data.mePersonId) ?? []),
    [people, settings.data],
  );

  // Seed the visible fields from the message's actual headers, not the identity
  // graph. A person may have several aliases, but a message was addressed to only
  // the addresses its headers name; expanding a recipient to every known alias
  // makes the visual default misleading and could send to people the message did
  // not include. Untouched lists are still omitted from requests, so Gmail remains
  // authoritative (notably for Reply-To) and its exact audience replaces these
  // header-backed estimates in the preview.
  const defaults = useMemo(() => {
    const seen = new Set(mine.map(addressKey));
    const to: Address[] = [];
    const cc: Address[] = [];
    const sender = answer.fromEmail ? addressKey(answer.fromEmail) : "";
    const offer = (list: Address[], who: Address) => {
      const key = addressKey(who.address);
      if (!key || seen.has(key) || key === sender) return;
      seen.add(key);
      list.push(who);
    };
    if (answer.fromEmail) {
      const key = addressKey(answer.fromEmail);
      if (!seen.has(key)) {
        seen.add(key);
        to.push({ name: answer.author ?? undefined, address: answer.fromEmail });
      }
    }
    for (const who of [...(answer.toRecipients ?? []), ...(answer.ccRecipients ?? [])]) {
      offer(cc, { name: who.name, address: who.address });
    }
    return { to, cc };
  }, [answer, mine]);

  // Load defaults after the people query resolves, and update the suggested Cc
  // when reply-all changes. Once a list is edited, it belongs to the reader.
  useEffect(() => {
    if (!toTouched) setTo(defaults.to);
    if (!ccTouched) setCc(all ? defaults.cc : []);
  }, [all, ccTouched, defaults, toTouched]);

  // The autocomplete offers addresses on this message first, then everybody else
  // the corpus knows. The explicit From header wins over a possibly stale identity
  // row; the mailbox's Reply-To remains authoritative whenever To is untouched.
  const suggestions = useMemo(() => {
    const out: Address[] = [];
    const seen = new Set<string>();
    const offer = (who: Address) => {
      const key = addressKey(who.address);
      if (!key || seen.has(key)) return;
      seen.add(key);
      out.push(who);
    };
    if (answer.fromEmail) offer({ name: answer.author ?? undefined, address: answer.fromEmail });
    for (const who of [...(answer.toRecipients ?? []), ...(answer.ccRecipients ?? [])])
      offer({ name: who.name, address: who.address });
    for (const p of answer.participants ?? [])
      for (const address of people.get(p.personId) ?? []) offer({ name: p.name, address });
    for (const p of roster.data?.people ?? [])
      for (const address of addressesOf(p.identities)) offer({ name: p.displayName, address });
    return out;
  }, [answer, people, roster.data]);

  // The first press: a read. The server composes the reply and answers with the
  // plan, and nothing is written to the mailbox — so a press that fails here, from
  // an entry the mailbox does not hold or a host without the grant, has cost the
  // reader nothing and left nothing behind.
  async function review() {
    setError(null);
    setBusy(true);
    try {
      const res = await send.mutateAsync({
        body: {
          entry: answer.extId,
          body: own,
          all,
          html,
          confirm: false,
          ...(toTouched ? { to: to.map((a) => a.address) } : {}),
          ...(ccTouched ? { cc: cc.map((a) => a.address) } : {}),
        },
      });
      setPlan(res);
      // While editing, the mailbox's response replaces any estimates from the
      // corpus with the exact addresses it will use. The preview itself is read-only.
      if (res.toRecipients) setTo(res.toRecipients);
      if (res.ccRecipients) setCc(res.ccRecipients);
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
  // Only edited lists are named in the send. Leaving one alone lets the mailbox
  // apply its own defaults (including Reply-To and account aliases), exactly as it
  // did for the preview. An edited list is sent as bare addresses: display names in
  // headers are a formatting question with their own rules, and a name the message
  // already carried is kept by the mailbox (see gmailclient's WithRecipients).
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
          // Only a list the reader edited is named explicitly. The other one
          // remains the mailbox's default, including Reply-To and account aliases.
          ...(toTouched ? { to: to.map((a) => a.address) } : {}),
          ...(ccTouched ? { cc: cc.map((a) => a.address) } : {}),
        },
      });
      unplan();
      setOwn("");
      setToTouched(false);
      setCcTouched(false);
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
            to <strong>{plan.to || "(no recipient)"}</strong>
            {plan.cc ? (
              <>
                , cc <strong>{plan.cc}</strong>
              </>
            ) : null}
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
          <div className="replyrecipients">
            <div className="replyrecipient">
              <span>to:</span>
              <AddressField
                label="to"
                value={to}
                onChange={changeTo}
                suggestions={suggestions}
                taken={cc}
                mine={mine}
                disabled={busy}
              />
            </div>
            <div className="replyrecipient">
              <span>cc:</span>
              <AddressField
                label="cc"
                value={cc}
                onChange={changeCc}
                suggestions={suggestions}
                taken={to}
                mine={mine}
                disabled={busy}
              />
            </div>
            <a
              className="opbtn par replytarget"
              href={`#${answerAnchor}`}
              aria-label={`Jump to the message being replied to: ${words.who || "the sender"}, ${words.when}`}
              title={`Jump to the message being replied to: ${words.whoTitle ?? words.who}, ${words.when}`}
            >
              <span className="arw" aria-hidden="true">&#8617;</span>
              <span>{words.who || "message"}</span>
            </a>
          </div>
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
                and what either leaves out can be put back in the address fields above,
                which is also where the audience grows. */}
            <div className="replyopts">
              <label
                className="replytick"
                title={
                  "Answer everyone the message was addressed to, not only whoever wrote it. " +
                  "Who that is comes from the message itself — the fields above are where " +
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
              disabled={busy || own.trim() === "" || (toTouched && to.length === 0)}
              title={toTouched && to.length === 0 ? "A reply needs somebody in to — put an address there first." : undefined}
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
