import { useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { $api, type CorpusEntry, type SendResponse } from "../lib/api";
import { dismissToast, pushToast } from "../lib/toasts";
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
 * **There is no recipient field, and that is the feature rather than a scoping
 * shortcut.** The box answers the newest message in this thread the mailbox holds,
 * the server takes the address from that message's own From header, and there is
 * nothing on this screen that can name a different one. That is what keeps a
 * surface behind a loopback bind with no authentication from being an outbound
 * channel to anywhere: mail can only be answered, never addressed — so a host that
 * leaves this on is one where a reader can reply to their own correspondence, not
 * one where something else can send mail as them.
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
 * their sentence should be able to see that before it leaves. What the box does not
 * offer is offered nowhere: no attachments, no cc, no drafts, no send-later.
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
export function ReplyBox({
  thread,
  answer,
  words,
}: {
  /** The thread being read, which is what is re-read once the answer is in it. */
  thread: { rootExtId: string };
  /** The message being answered: the newest entry of this thread the mailbox
   *  holds. One entry rather than the thread, because a reply is to a message —
   *  and a message the mailbox does not hold (a line recovered from somebody
   *  else's quote, a Slack post) cannot be answered, because there is nothing to
   *  thread an answer onto. The caller picks it and the server refuses anything
   *  that cannot be answered, so the two rules are kept in one place each rather
   *  than agreed twice. */
  answer: CorpusEntry;
  /** How that message's own bubble names it — the name and clock its head wears
   *  (see ThreadMessages' stamp words). Passed in rather than written again here:
   *  the box says "replying to Lane Whittaker, Mon 2 Mar 2026 09:15", and a reader
   *  looking at the same message in the same pane must not be told a different
   *  clock by the reply box than by the bubble above it. */
  words: { who: string; when: string };
}) {
  const queryClient = useQueryClient();
  // What the reader has written, in their own words: plain text, and no quote of
  // the message being answered — that is added on the way out, so the reader is
  // never editing around text they did not write.
  const [own, setOwn] = useState("");
  // The plan the preview came back with, while the reader is looking at it.
  const [plan, setPlan] = useState<SendResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

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

  // The first press: a read. The server composes the reply and answers with the
  // plan, and nothing is written to the mailbox — so a press that fails here, from
  // an entry the mailbox does not hold or a host without the grant, has cost the
  // reader nothing and left nothing behind.
  async function review() {
    setError(null);
    setBusy(true);
    try {
      const res = await send.mutateAsync({
        body: { entry: answer.extId, body: own, confirm: false },
      });
      setPlan(res);
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
  // deliberately not recomposed here, so what goes out is what was read.
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
        body: { entry: answer.extId, body: own, confirm: true },
      });
      setPlan(null);
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
    <div className="replybox">
      <p className="replyto">
        Replying to {words.who || "the sender"}
        {words.when ? `, ${words.when}` : ""} — the newest message here the mailbox holds.
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
            it will go: to <strong>{plan.to}</strong>, as <strong>{plan.subject}</strong>. Your words come first and
            the message you are answering is quoted under them.
          </p>
          {/* The body as it will be sent, whitespace and all: a quote is line by
              line, and a reply that reflowed on its way out would not be this
              text. */}
          <pre className="replytext">{plan.body}</pre>
          <div className="opmact">
            <button
              type="button"
              className="opbtn opbtn-after"
              disabled={busy}
              onClick={() => void ship()}
            >
              {busy ? "sending…" : "send this reply"}
            </button>
            <button
              type="button"
              className="opbtn"
              disabled={busy}
              onClick={() => setPlan(null)}
            >
              keep editing
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
          <div className="opmact">
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
