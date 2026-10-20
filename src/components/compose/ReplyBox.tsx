import { useCallback, useEffect, useReducer, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { $api, type CorpusEntry, type SendResponse } from "../../lib/api/api";
import { useAccountId } from "../../lib/inbox/useAccountId";
import { staleAfterMail } from "../../lib/inbox/mailActions";
import { errText } from "../../lib/ui/errText";
import useOwnToast from "../../lib/ui/useOwnToast";
import ComposerFields from "./ComposerFields";
import { parseAddressHeader, type Draft } from "./Draft";
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
import ReviewInput, { type ReviewedInput } from "../../lib/message/reviewedInput";
import DraftOwnership, { removeCachedDraft } from "../../lib/message/draftOwnership";
import type { components } from "../../lib/api/api.d";

type ReplyInput = Omit<components["schemas"]["SendRequest"], "confirm">;

type Step =
  | { kind: "editing" }
  | {
      kind: "reviewing";
      plan: SendResponse;
      reviewed: ReviewedInput<ReplyInput>;
      sendFailed: boolean;
    };

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
  | { type: "planned"; plan: SendResponse; reviewed: ReviewedInput<ReplyInput> }
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
        step: {
          kind: "reviewing",
          plan: action.plan,
          reviewed: action.reviewed,
          sendFailed: false,
        },
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
  const [draftId, setDraftId] = useState("");
  const [draftSaveError, setDraftSaveError] = useState("");
  const draftIdRef = useRef("");
  const draftMessageIdRef = useRef("");
  const saveGeneration = useRef(0);
  const saveQueue = useRef(Promise.resolve());
  const drafts = $api.useQuery(
    "get",
    "/v1/drafts",
    {
      params: { query: displayedAccountId ? { accountId: displayedAccountId } : {} },
    },
    { enabled: Boolean(displayedAccountId) },
  );
  const createDraft = $api.useMutation("post", "/v1/drafts");
  const updateDraft = $api.useMutation("put", "/v1/drafts/{id}");
  const deleteDraft = $api.useMutation("delete", "/v1/drafts/{id}");
  const recipients = useReplyRecipients(answer, all, displayedAccountId);
  const ownership = useRef(new DraftOwnership()).current;
  ownership.enter(JSON.stringify([answer.extId, displayedAccountId]), Boolean(own));
  const { to, cc } = recipients;
  const restoreRecipients = recipients.restore;
  const { busy, error, clearError, run } = useBusyTask();
  const send = $api.useMutation("post", "/v1/send");
  // The box isn't remounted between threads, so its toast goes when the thread changes.
  const say = useOwnToast(thread.rootExtId);
  const sendFailed = step.kind === "reviewing" && step.sendFailed;
  const reviewInput = useRef(new ReviewInput<ReplyInput>()).current;
  reviewInput.update({
    entry: answer.extId,
    body: own,
    all,
    html,
    ...(displayedAccountId ? { accountId: displayedAccountId } : {}),
    ...recipients.edited,
  });
  const previewIsCurrent = step.kind === "reviewing" && reviewInput.isCurrent(step.reviewed);
  useEffect(
    () => () => {
      reviewInput.invalidate();
      saveGeneration.current++;
    },
    [reviewInput],
  );
  // Replies have no subject field; the server derives it.
  const draft: Draft = { to, cc, subject: "", body: own };
  const onDraft = (patch: Partial<Draft>) => {
    ownership.edit();
    if (patch.to) recipients.changeTo(patch.to);
    if (patch.cc) recipients.changeCc(patch.cc);
    if (patch.body !== undefined) dispatch({ type: "words", own: patch.body });
  };
  // A ref rather than a document query: this is one box among any the shell has drawn.
  const host = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    dispatch({ type: "retarget", accountId: routeAccountId ?? "" });
    saveGeneration.current++;
    setDraftSaveError("");
    draftIdRef.current = "";
    draftMessageIdRef.current = "";
    setDraftId("");
  }, [answer.extId, routeAccountId]);

  // Gmail associates a reply draft with its thread. Resume it automatically when
  // the selected thread/account changes, rather than starting a second draft.
  const matchingDraft = drafts.data?.find(
    (item) =>
      item.accountId === displayedAccountId &&
      item.threadId &&
      item.threadId === answer.container &&
      !ownership.isRetired(item.accountId, item.id),
  );
  const draftConflict = Boolean(matchingDraft && matchingDraft.id !== draftId);
  const resumeDraft = useCallback(() => {
    if (!matchingDraft) return;
    saveGeneration.current++;
    setDraftSaveError("");
    draftIdRef.current = matchingDraft.id;
    draftMessageIdRef.current = matchingDraft.messageId ?? "";
    setDraftId(matchingDraft.id);
    dispatch({ type: "words", own: matchingDraft.body ?? "" });
    restoreRecipients(parseAddressHeader(matchingDraft.to), parseAddressHeader(matchingDraft.cc));
  }, [matchingDraft, restoreRecipients]);
  useEffect(() => {
    if (!matchingDraft || matchingDraft.id === draftIdRef.current || !ownership.canHydrate())
      return;
    resumeDraft();
  }, [matchingDraft, ownership, resumeDraft]);

  // Autosave to Gmail. Queue writes so an older request cannot land after a newer
  // edit and replace it; a draft ID returned by create is reused by later saves.
  useEffect(() => {
    if (
      !own.trim() ||
      !displayedAccountId ||
      !answer.extId ||
      drafts.isPending ||
      drafts.isError ||
      draftSaveError ||
      busy ||
      draftConflict ||
      step.kind !== "editing"
    )
      return;
    const words = own;
    const edited = recipients.edited;
    const generation = saveGeneration.current;
    const subject = answer.subject?.startsWith("Re:")
      ? answer.subject
      : `Re: ${answer.subject ?? ""}`;
    const timer = window.setTimeout(() => {
      saveQueue.current = saveQueue.current
        .catch(() => undefined)
        .then(async () => {
          if (generation !== saveGeneration.current) return;
          const id = draftIdRef.current;
          const body = {
            accountId: displayedAccountId,
            ...edited,
            replyAll: all,
            subject,
            body: words,
            replyToEntry: answer.extId,
            ...(id ? { expectedMessageId: draftMessageIdRef.current } : {}),
          };
          const saved = id
            ? await updateDraft.mutateAsync({ params: { path: { id } }, body })
            : await createDraft.mutateAsync({ body });
          if (generation !== saveGeneration.current) return;
          draftIdRef.current = saved.id;
          draftMessageIdRef.current = saved.messageId ?? "";
          setDraftId(saved.id);
          setDraftSaveError("");
          await queryClient.invalidateQueries({ queryKey: ["get", "/v1/drafts"] });
        })
        .catch((error) => {
          console.error("Saving Gmail reply draft failed", error);
          if (generation !== saveGeneration.current) return;
          setDraftSaveError(
            `${errText(error)} Autosave stopped; check Gmail before resuming this draft.`,
          );
        });
    }, 700);
    return () => window.clearTimeout(timer);
  }, [
    own,
    draftSaveError,
    busy,
    recipients.edited,
    all,
    displayedAccountId,
    draftConflict,
    step.kind,
    answer.subject,
    answer.extId,
    drafts.isPending,
    drafts.isError,
    createDraft,
    updateDraft,
    queryClient,
  ]);

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
        const reviewed = reviewInput.capture();
        const plan = await send.mutateAsync({ body: { ...reviewed.input, confirm: false } });
        if (!reviewInput.isCurrent(reviewed)) return;
        dispatch({ type: "planned", plan, reviewed });
        recipients.adopt(plan);
      },
      (e) => refusal(e, "-send-mail", "Preparing the reply"),
    );

  // The server files the sent message into the corpus in the same request, so the
  // thread is re-read rather than drawn optimistically.
  async function ship() {
    if (step.kind !== "reviewing" || !previewIsCurrent) return;
    const { plan, reviewed } = step;
    await run(
      async () => {
        await saveQueue.current;
        saveGeneration.current++;
        if (!reviewInput.isCurrent(reviewed)) return;
        const savedDraftId = draftIdRef.current;
        const generation = saveGeneration.current;
        await send.mutateAsync({
          body: { ...reviewed.input, confirm: true, accountId: plan.accountId },
        });
        let draftCleanupFailed = false;
        if (savedDraftId) {
          ownership.retire(plan.accountId ?? displayedAccountId, savedDraftId);
          removeCachedDraft(queryClient, plan.accountId ?? displayedAccountId, savedDraftId);
          try {
            await deleteDraft.mutateAsync({
              params: { path: { id: savedDraftId }, query: { accountId: plan.accountId } },
            });
          } catch (e) {
            console.error("Removing the sent reply draft failed", e);
            draftCleanupFailed = true;
          }
          if (generation === saveGeneration.current && draftIdRef.current === savedDraftId) {
            draftIdRef.current = "";
            draftMessageIdRef.current = "";
            setDraftId("");
          }
        }
        if (reviewInput.isCurrent(reviewed)) {
          reviewInput.invalidate();
          dispatch({ type: "sent" });
          recipients.untouch();
        }
        void queryClient.invalidateQueries({ queryKey: ["get", "/v1/drafts"] });
        say(
          draftCleanupFailed
            ? "Reply sent, but its Gmail draft could not be removed; delete that leftover draft in Gmail."
            : `Answered ${words.who || "the sender"} — sent, and filed in the trail below.`,
        );
      },
      (e) => {
        // The request may have reached Gmail even if its response did not; a retry could duplicate mail.
        dispatch({ type: "sendFailed" });
        return refusal(e, "-send-mail", "Sending the reply");
      },
    );
    staleAfterMail(queryClient);
  }

  async function discardDraft() {
    const id = draftIdRef.current;
    if (!id || !window.confirm("Discard this Gmail reply draft?")) return;
    await run(async () => {
      await saveQueue.current;
      const generation = ++saveGeneration.current;
      await deleteDraft.mutateAsync({
        params: { path: { id }, query: { accountId: displayedAccountId } },
      });
      ownership.retire(displayedAccountId, id);
      removeCachedDraft(queryClient, displayedAccountId, id);
      if (generation !== saveGeneration.current) return;
      draftIdRef.current = "";
      draftMessageIdRef.current = "";
      setDraftId("");
      setDraftSaveError("");
      dispatch({ type: "words", own: "" });
      recipients.untouch();
      await queryClient.invalidateQueries({ queryKey: ["get", "/v1/drafts"] });
    }, errText);
  }

  const errorAlert = error ? (
    <InlineAlert>
      {error}
      {sendFailed ? " Check Gmail before attempting to send again." : ""}
    </InlineAlert>
  ) : null;

  return (
    <div className="mt-4 mb-1 rounded-lg border border-line bg-card px-3 py-2" ref={host}>
      {draftSaveError ? <InlineAlert>{draftSaveError}</InlineAlert> : null}
      {draftConflict && !ownership.canHydrate() ? (
        <InlineAlert>
          A saved Gmail draft exists. Your new words have not been replaced or autosaved.
          <Button
            type="button"
            variant="subtle"
            density="compact"
            disabled={busy}
            onClick={() => {
              if (window.confirm("Replace your unsaved words with the saved Gmail draft?"))
                resumeDraft();
            }}
          >
            Resume saved draft
          </Button>
        </InlineAlert>
      ) : null}
      {draftId ? <p className="mb-1 text-2xs text-muted">Saved to Gmail</p> : null}
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
                  reviewInput.invalidate();
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
                  disabled={busy || !previewIsCurrent || to.length === 0}
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
                  onChange={(id) => {
                    ownership.edit();
                    saveGeneration.current++;
                    setDraftSaveError("");
                    draftIdRef.current = "";
                    draftMessageIdRef.current = "";
                    setDraftId("");
                    dispatch({ type: "account", accountId: id });
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
                  className="flex cursor-pointer items-center gap-1.5 text-xs text-muted has-disabled:cursor-default has-disabled:opacity-55"
                  title="Answer everyone the message was addressed to. Edit the reply's audience in the fields above."
                  checked={all}
                  disabled={busy}
                  accent="accent"
                  inputClassName="m-0 cursor-pointer"
                  onChange={(e) => {
                    ownership.edit();
                    onAll(e.target.checked);
                  }}
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
              {draftId ? (
                <Button
                  variant="danger"
                  density="compact"
                  type="button"
                  disabled={busy}
                  onClick={() => void discardDraft()}
                >
                  Discard draft
                </Button>
              ) : null}
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
