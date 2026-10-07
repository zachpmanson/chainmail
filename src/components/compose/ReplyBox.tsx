import { useEffect, useReducer, useRef } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { $api, type CorpusEntry, type SendResponse } from "../../lib/api/api";
import { useAccountId } from "../../lib/inbox/useAccountId";
import { staleAfterMail } from "../../lib/inbox/mailActions";
import useOwnToast from "../../lib/ui/useOwnToast";
import ComposerFields from "./ComposerFields";
import type { Draft } from "./Draft";
import { refusal } from "../inbox/MailVerbs";
import CheckboxRow from "../ui/CheckboxRow";
import Button from "../ui/Button";
import InlineAlert from "../ui/InlineAlert";
import AccountSelect from "./AccountSelect";
import ReplyPlan from "./ReplyPlan";
import ReplyTarget from "./ReplyTarget";
import useAccounts from "./useAccounts";
import useBusyTask from "./useBusyTask";
import useReplyRecipients from "./useReplyRecipients";

type Step = { kind: "editing" } | { kind: "reviewing"; plan: SendResponse; sendFailed: boolean };

type State = {
  /** The reader's words alone; the quote is added on the way out. */
  own: string;
  html: boolean;
  accountId: string;
  /** The recipient fields and sender selector start closed behind a summary line. */
  editingRecipients: boolean;
  step: Step;
};

type Action =
  | { type: "retarget"; accountId: string }
  | { type: "words"; own: string }
  | { type: "html"; html: boolean }
  | { type: "account"; accountId: string }
  | { type: "editRecipients" }
  | { type: "planned"; plan: SendResponse }
  | { type: "edit" }
  | { type: "sent" }
  | { type: "sendFailed" };

const editing: Step = { kind: "editing" };

function reduce(state: State, action: Action): State {
  switch (action.type) {
    // A plan quotes the message it was made for, so retargeting drops it; the words stay.
    case "retarget":
      return { ...state, accountId: action.accountId, editingRecipients: false, step: editing };
    case "words":
      return { ...state, own: action.own };
    case "html":
      return { ...state, html: action.html };
    case "account":
      return { ...state, accountId: action.accountId, step: editing };
    case "editRecipients":
      return { ...state, editingRecipients: true };
    case "planned":
      return {
        ...state,
        accountId: action.plan.accountId || state.accountId,
        step: { kind: "reviewing", plan: action.plan, sendFailed: false },
      };
    case "edit":
      return { ...state, step: editing };
    case "sent":
      return { ...state, own: "", editingRecipients: false, step: editing };
    case "sendFailed":
      return state.step.kind === "reviewing"
        ? { ...state, step: { ...state.step, sendFailed: true } }
        : state;
  }
}

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
  const routeAccountId = useAccountId();
  const [{ own, html, accountId, editingRecipients, step }, dispatch] = useReducer(reduce, {
    own: "",
    html: true,
    accountId: routeAccountId ?? "",
    editingRecipients: false,
    step: editing,
  });
  const { connected } = useAccounts();
  const displayedAccountId = accountId || (connected.length === 1 ? (connected[0]?.id ?? "") : "");
  const recipients = useReplyRecipients(answer, all, routeAccountId);
  const { to, cc } = recipients;
  const { busy, error, clearError, run } = useBusyTask();
  const send = $api.useMutation("post", "/v1/send");
  // The box isn't remounted between threads, so its toast goes when the thread changes.
  const say = useOwnToast(thread.rootExtId);
  const sendFailed = step.kind === "reviewing" && step.sendFailed;
  // Replies have no subject field; the server derives it.
  const draft: Draft = { to, cc, subject: "", body: own };
  const onDraft = (patch: Partial<Draft>) => {
    if (patch.to) recipients.changeTo(patch.to);
    if (patch.cc) recipients.changeCc(patch.cc);
    if (patch.body !== undefined) dispatch({ type: "words", own: patch.body });
  };
  // A ref rather than a document query: this is one box among any the shell has drawn.
  const host = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    dispatch({ type: "retarget", accountId: routeAccountId ?? "" });
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
        const plan = await send.mutateAsync({
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
        dispatch({ type: "planned", plan });
        recipients.adopt(plan);
      },
      (e) => refusal(e, "-send-mail", "Preparing the reply"),
    );

  // The server files the sent message into the corpus in the same request, so the
  // thread is re-read rather than drawn optimistically.
  async function ship() {
    if (step.kind !== "reviewing") return;
    const { plan } = step;
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
        dispatch({ type: "sent" });
        recipients.untouch();
        say(`Answered ${words.who || "the sender"} — sent, and filed in the trail below.`);
      },
      (e) => {
        // The request may have reached Gmail even if its response did not; a retry could duplicate mail.
        dispatch({ type: "sendFailed" });
        return refusal(e, "-send-mail", "Sending the reply");
      },
    );
    staleAfterMail(queryClient);
  }

  const errorAlert = error ? (
    <InlineAlert>
      {error}
      {sendFailed ? " Check Gmail before attempting to send again." : ""}
    </InlineAlert>
  ) : null;

  return (
    <div className="mt-4 mb-1 rounded-lg border border-line bg-card px-3 py-2" ref={host}>
      <section aria-label="Reply">
        {step.kind === "reviewing" ? (
          <>
            <ReplyPlan plan={step.plan} />
            {errorAlert}
            <footer className="mt-2 flex items-center justify-end gap-2">
              <Button
                variant="subtle"
                density="compact"
                type="button"
                disabled={busy}
                onClick={() => {
                  dispatch({ type: "edit" });
                  clearError();
                }}
              >
                {busy ? "Working…" : "keep editing"}
              </Button>
              {sendFailed ? null : (
                <Button
                  variant="danger"
                  density="compact"
                  type="button"
                  disabled={busy || to.length === 0}
                  onClick={() => void ship()}
                >
                  {busy ? "Sending…" : "send this reply"}
                </Button>
              )}
            </footer>
          </>
        ) : (
          <>
            {errorAlert}
            <ComposerFields
              mode={{
                kind: "reply",
                editingRecipients,
                onEditRecipients: () => dispatch({ type: "editRecipients" }),
                target: <ReplyTarget answerAnchor={answerAnchor} words={words} />,
              }}
              from={
                <AccountSelect
                  value={displayedAccountId}
                  disabled={busy}
                  onChange={(id) => dispatch({ type: "account", accountId: id })}
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
                  className="flex cursor-pointer items-center gap-1.5 text-xs text-muted has-disabled:cursor-default has-disabled:opacity-55"
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
                  className="flex cursor-pointer items-center gap-1.5 text-xs text-muted has-disabled:cursor-default has-disabled:opacity-55"
                  title="Send the HTML rendering beside the same plain-text message. Unchecked sends plain text alone."
                  checked={html}
                  disabled={busy}
                  accent="accent"
                  inputClassName="m-0 cursor-pointer"
                  onChange={(e) => dispatch({ type: "html", html: e.target.checked })}
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
