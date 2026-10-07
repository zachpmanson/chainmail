import { useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useSearch } from "@tanstack/react-router";
import { $api, type CorpusEntry, type SendResponse } from "../../lib/api/api";
import { staleAfterMail } from "../../lib/inbox/mailActions";
import { dismissToast, pushToast } from "../../lib/ui/toasts";
import ComposerFields from "./ComposerFields";
import type { Draft } from "./Draft";
import { refusal, SAID_MS } from "../inbox/MailVerbs";
import CheckboxRow from "../ui/CheckboxRow";
import { Button } from "../ui/controls";
import InlineAlert from "../ui/InlineAlert";
import AccountSelect from "./AccountSelect";
import ReplyPlan from "./ReplyPlan";
import ReplyTarget from "./ReplyTarget";
import useAccounts from "./useAccounts";
import useBusyTask from "./useBusyTask";
import useReplyRecipients from "./useReplyRecipients";

/**
 * Answers the newest answerable message unless AnswerPress names another. The quote is
 * the server's (ReplyBody); `preview` only reads, and `send this reply` sends what was shown.
 */
export default function ReplyBox({
  thread,
  answer,
  answerAnchor,
  words,
  all,
  onAll,
  aimed,
}: {
  thread: { rootExtId: string };
  answer: CorpusEntry;
  answerAnchor: string;
  words: { who: string; whoTitle?: string; when: string };
  all: boolean;
  onAll: (all: boolean) => void;
  aimed: number;
}) {
  const queryClient = useQueryClient();
  const routeAccountId = useSearch({ from: "/" }).accountId;
  const [accountId, setAccountId] = useState(routeAccountId ?? "");
  const { connected } = useAccounts();
  const displayedAccountId = accountId || (connected.length === 1 ? (connected[0]?.id ?? "") : "");
  // The reader's words alone; the quote is added on the way out.
  const [own, setOwn] = useState("");
  const [html, setHtml] = useState(true);
  const [plan, setPlan] = useState<SendResponse | null>(null);
  // The recipient fields and sender selector start closed behind a summary line.
  const [editing, setEditing] = useState(false);
  const recipients = useReplyRecipients(answer, all, routeAccountId);
  const { to, cc } = recipients;
  const { busy, error, run } = useBusyTask();
  const send = $api.useMutation("post", "/v1/send");
  const say = useReplyToast(thread.rootExtId);
  // Replies have no subject field; the server derives it.
  const draft: Draft = { to, cc, subject: "", body: own };
  const onDraft = (patch: Partial<Draft>) => {
    if (patch.to) recipients.changeTo(patch.to);
    if (patch.cc) recipients.changeCc(patch.cc);
    if (patch.body !== undefined) setOwn(patch.body);
  };
  // A ref rather than a document query: this is one box among any the shell has drawn.
  const host = useRef<HTMLDivElement | null>(null);

  // A plan quotes the message it was made for, so retargeting drops it; the words stay.
  useEffect(() => {
    setPlan(null);
    setAccountId(routeAccountId ?? "");
    setEditing(false);
  }, [answer.extId, routeAccountId]);

  // A press on a message's answer control brings the box, which may be screens away, to the reader.
  useEffect(() => {
    if (!aimed) return;
    host.current?.scrollIntoView?.({ block: "center" });
    host.current?.querySelector<HTMLTextAreaElement>("textarea")?.focus();
  }, [aimed]);

  // Nothing is written to the mailbox, so a failure here has cost nothing.
  const review = () =>
    run(
      async () => {
        const res = await send.mutateAsync({
          body: {
            entry: answer.extId,
            body: own,
            all,
            html,
            confirm: false,
            ...(displayedAccountId ? { accountId: displayedAccountId } : {}),
            ...recipients.edited,
          },
        });
        setPlan(res);
        if (res.accountId) setAccountId(res.accountId);
        recipients.adopt(res);
      },
      (e) => refusal(e, "-send-mail", "Preparing the reply"),
    );

  // The server files the sent message into the corpus in the same request, so the
  // thread is re-read rather than drawn optimistically.
  async function ship() {
    if (!plan) return;
    await run(
      async () => {
        await send.mutateAsync({
          body: {
            entry: answer.extId,
            body: own,
            all,
            html,
            confirm: true,
            accountId: plan.accountId,
            ...recipients.edited,
          },
        });
        setPlan(null);
        setOwn("");
        recipients.untouch();
        setEditing(false);
        say(`Answered ${words.who || "the sender"} — sent, and filed in the trail below.`);
      },
      // The plan stays on screen; the server's message says whether it may have sent.
      (e) => refusal(e, "-send-mail", "Sending the reply"),
    );
    staleAfterMail(queryClient);
  }

  const errorAlert = error ? <InlineAlert>{error}</InlineAlert> : null;

  return (
    <div className="mt-4 mb-1 rounded-lg border border-line bg-card px-3 py-2" ref={host}>
      <section aria-label="Reply">
        {plan ? (
          <>
            <ReplyPlan plan={plan} />
            {errorAlert}
            <footer className="mt-2 flex items-center justify-end gap-2">
              <Button
                variant="subtle"
                density="compact"
                type="button"
                disabled={busy}
                onClick={() => setPlan(null)}
              >
                {busy ? "Working…" : "keep editing"}
              </Button>
              <Button
                variant="danger"
                density="compact"
                type="button"
                disabled={busy || to.length === 0}
                onClick={() => void ship()}
              >
                {busy ? "Sending…" : "send this reply"}
              </Button>
            </footer>
          </>
        ) : (
          <>
            {errorAlert}
            <ComposerFields
              mode={{
                kind: "reply",
                editingRecipients: editing,
                onEditRecipients: () => setEditing(true),
                target: <ReplyTarget answerAnchor={answerAnchor} words={words} />,
              }}
              from={
                <AccountSelect
                  value={displayedAccountId}
                  disabled={busy}
                  onChange={(id) => {
                    setAccountId(id);
                    setPlan(null);
                  }}
                />
              }
              draft={draft}
              onDraft={onDraft}
              suggestions={recipients.suggestions}
              mine={recipients.mine}
              busy={busy}
            />
            <footer className="mt-2 flex items-center justify-end gap-2">
              <div className="mr-auto flex items-center gap-4">
                <CheckboxRow
                  className="flex cursor-pointer items-center gap-1.5 text-xs text-muted has-disabled:cursor-default has-disabled:opacity-[.55]"
                  title="Answer everyone the message was addressed to. Edit the reply's audience in the fields above."
                  checked={all}
                  disabled={busy}
                  accent="accent"
                  inputClassName="m-0 cursor-pointer"
                  onChange={(e) => onAll(e.target.checked)}
                >
                  reply all
                </CheckboxRow>
                <CheckboxRow
                  className="flex cursor-pointer items-center gap-1.5 text-xs text-muted has-disabled:cursor-default has-disabled:opacity-[.55]"
                  title="Send the HTML rendering beside the same plain-text message. Unchecked sends plain text alone."
                  checked={html}
                  disabled={busy}
                  accent="accent"
                  inputClassName="m-0 cursor-pointer"
                  onChange={(e) => setHtml(e.target.checked)}
                >
                  send html
                </CheckboxRow>
              </div>
              <Button
                variant="subtle"
                density="compact"
                type="button"
                title={
                  to.length === 0 ? "Add somebody in to before previewing this reply." : undefined
                }
                disabled={busy || own.trim() === "" || (recipients.toTouched && to.length === 0)}
                onClick={() => void review()}
              >
                {busy ? "Preparing…" : "preview"}
              </Button>
            </footer>
          </>
        )}
      </section>
    </div>
  );
}

/** The box isn't remounted between threads, so its toast goes when the thread changes; each send replaces the last. */
function useReplyToast(threadKey: string) {
  const said = useRef<number | null>(null);
  useEffect(() => {
    if (said.current !== null) dismissToast(said.current);
    said.current = null;
  }, [threadKey]);
  return (text: string) => {
    if (said.current !== null) dismissToast(said.current);
    said.current = pushToast(text, "note", SAID_MS);
  };
}
