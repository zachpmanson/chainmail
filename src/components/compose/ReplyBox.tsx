import DraftSaveError from "./DraftSaveError";
import DraftCompatibilityNotice from "./DraftCompatibilityNotice";
import BodyWordCount from "./BodyWordCount";
import { addressWords } from "./AddressField";
import DraftCards from "./DraftCards";
import useSelectedDraft from "./useSelectedDraft";
import { draftDestination, draftIdentity } from "../../lib/ui/draftSelection";
import { useEffect, useMemo, useReducer, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { $api, type CorpusEntry, type SendResponse } from "../../lib/api/api";
import { useAccountId } from "../../lib/inbox/useAccountId";
import { staleAfterMail } from "../../lib/inbox/mailActions";
import { errText } from "../../lib/ui/errText";
import useOwnToast from "../../lib/ui/useOwnToast";
import ComposerFields from "./ComposerFields";
import DraftReadError from "./DraftReadError";
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
  /** The editable text; restored Gmail drafts also carry their saved quote. */
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
  scopePending = false,
}: {
  thread: { rootExtId: string };
  answer: CorpusEntry;
  answerAnchor: string;
  words: { who: string; whoTitle?: string; when: string };
  all: boolean;
  onAll: (all: boolean) => void;
  aimed: number;
  scopePending?: boolean;
}) {
  const scopePendingRef = useRef(scopePending);
  scopePendingRef.current = scopePending;
  const queryClient = useQueryClient();
  const routeAccountId = useAccountId();
  const continuityAccount = useRef(routeAccountId ?? "");
  const transferAccount = useRef<string>();
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
  const acknowledgedSaveRef = useRef("");
  const draftIdRef = useRef("");
  const draftMessageIdRef = useRef("");
  const saveGeneration = useRef(0);
  const saveQueue = useRef(Promise.resolve());
  const pendingWrites = useRef(0);
  const writeRevision = useRef(0);
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
  const { busy, busyRef, operationRevision, error, clearError, run } = useBusyTask();
  const send = $api.useMutation("post", "/v1/send");
  // The box isn't remounted between threads, so its toast goes when the thread changes.
  const say = useOwnToast(thread.rootExtId);
  const draftInput = useMemo(
    () => ({
      accountId: displayedAccountId,
      ...recipients.edited,
      replyAll: all,
      subject: answer.subject?.startsWith("Re:") ? answer.subject : `Re: ${answer.subject ?? ""}`,
      body: own,
      replyToEntry: answer.extId,
    }),
    [displayedAccountId, recipients.edited, all, answer.subject, own, answer.extId],
  );
  const draftInputKey = JSON.stringify(draftInput);
  const recoveryDraft = {
    to: to.map(addressWords),
    cc: cc.map(addressWords),
    subject: draftInput.subject,
    body: own,
  };
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
    continuityAccount.current = displayedAccountId;
    ownership.edit();
    selected.edit();
    if (patch.to) recipients.changeTo(patch.to);
    if (patch.cc) recipients.changeCc(patch.cc);
    if (patch.body !== undefined) dispatch({ type: "words", own: patch.body });
  };
  // A ref rather than a document query: this is one box among any the shell has drawn.
  const host = useRef<HTMLDivElement | null>(null);

  const effectiveRouteAccountId =
    routeAccountId ?? (connected.length === 1 ? (connected[0]?.id ?? "") : "");
  useEffect(() => {
    dispatch({ type: "retarget", accountId: effectiveRouteAccountId });
    saveGeneration.current++;
    setDraftSaveError("");
    draftIdRef.current = "";
    draftMessageIdRef.current = "";
    setDraftId("");
  }, [answer.extId, effectiveRouteAccountId]);

  const selected = useSelectedDraft({
    kind: "reply",
    account: displayedAccountId,
    id: draftId,
    target: answer.extId,
    snapshot: () => ({
      kind: "reply",
      destination: thread.rootExtId,
      target: answer.extId,
      revision: JSON.stringify([
        draftInputKey,
        all,
        html,
        step.kind,
        draftSaveError,
        sendFailed,
        operationRevision.current,
        writeRevision.current,
      ]),
      owner: draftIdRef.current ? draftIdentity(displayedAccountId, draftIdRef.current) : undefined,
      continuityAccountId: continuityAccount.current,
      transferAccountId: transferAccount.current,
      busy:
        busyRef.current ||
        pendingWrites.current > 0 ||
        createDraft.isPending ||
        updateDraft.isPending ||
        deleteDraft.isPending,
      dirty: Boolean(
        own ||
        recipients.edited.to ||
        recipients.edited.cc ||
        !all ||
        !html ||
        draftSaveError ||
        sendFailed,
      ),
      invalidate: () => {
        saveGeneration.current++;
        reviewInput.invalidate();
      },
    }),
    hydrate: (saved) => {
      continuityAccount.current = saved.accountId;
      transferAccount.current = undefined;
      saveGeneration.current++;
      reviewInput.invalidate();
      dispatch({ type: "edit" });
      setDraftSaveError("");
      draftIdRef.current = saved.id;
      draftMessageIdRef.current = saved.messageId ?? "";
      setDraftId(saved.id);
      const to = parseAddressHeader(saved.to);
      const cc = parseAddressHeader(saved.cc);
      dispatch({ type: "words", own: saved.body ?? "" });
      restoreRecipients(to, cc);
      const input = {
        accountId: displayedAccountId,
        to: to.map((a) => a.address),
        cc: cc.map((a) => a.address),
        replyAll: all,
        subject: answer.subject?.startsWith("Re:") ? answer.subject : `Re: ${answer.subject ?? ""}`,
        body: saved.body ?? "",
        replyToEntry: answer.extId,
      };
      const acknowledgement = JSON.stringify([
        saveGeneration.current,
        saved.id,
        JSON.stringify(input),
      ]);
      acknowledgedSaveRef.current = acknowledgement;
    },
  });

  // Autosave to Gmail. Queue writes so an older request cannot land after a newer
  // edit and replace it; a draft ID returned by create is reused by later saves.
  useEffect(() => {
    if (
      scopePending ||
      (!own.trim() && !draftIdRef.current) ||
      !displayedAccountId ||
      !answer.extId ||
      drafts.isPending ||
      drafts.isError ||
      draftSaveError ||
      busy ||
      selected.blocked ||
      step.kind !== "editing"
    )
      return;
    const generation = saveGeneration.current;
    if (
      acknowledgedSaveRef.current ===
      JSON.stringify([generation, draftIdRef.current, draftInputKey])
    )
      return;
    const timer = window.setTimeout(() => {
      writeRevision.current++;
      pendingWrites.current++;
      saveQueue.current = saveQueue.current
        .catch(() => undefined)
        .then(async () => {
          if (generation !== saveGeneration.current || scopePendingRef.current) return;
          const id = draftIdRef.current;
          if (acknowledgedSaveRef.current === JSON.stringify([generation, id, draftInputKey]))
            return;
          const body = {
            ...draftInput,
            ...(id ? { expectedMessageId: draftMessageIdRef.current } : {}),
          };
          const saved = id
            ? await updateDraft.mutateAsync({ params: { path: { id } }, body })
            : await createDraft.mutateAsync({ body });
          if (generation !== saveGeneration.current) return;
          draftIdRef.current = saved.id;
          draftMessageIdRef.current = saved.messageId ?? "";
          // A provider-normalised response must not acknowledge newer editor input.
          const acknowledgement = JSON.stringify([generation, saved.id, draftInputKey]);
          acknowledgedSaveRef.current = acknowledgement;
          setDraftId(saved.id);
          setDraftSaveError("");
          await queryClient.invalidateQueries({ queryKey: ["get", "/v1/drafts"] });
        })
        .catch((error) => {
          console.error("Saving Gmail reply draft failed", error);
          if (generation !== saveGeneration.current) return;
          // Cancel work already queued behind this failed write, before React renders the error.
          saveGeneration.current++;
          setDraftSaveError(
            `${errText(error)} Autosave stopped; check Gmail before resuming this draft.`,
          );
        })
        .finally(() => {
          pendingWrites.current--;
        });
    }, 700);
    return () => window.clearTimeout(timer);
  }, [
    own,
    selected.blocked,
    scopePending,
    draftSaveError,
    busy,
    draftInput,
    draftInputKey,
    displayedAccountId,
    step.kind,
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
    scopePendingRef.current || selected.blocked
      ? Promise.resolve()
      : run(
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
    if (scopePendingRef.current) return;
    if (selected.blocked || step.kind !== "reviewing" || !previewIsCurrent) return;
    const { plan, reviewed } = step;
    let sending = false;
    await run(
      async () => {
        await saveQueue.current;
        saveGeneration.current++;
        if (!reviewInput.isCurrent(reviewed)) return;
        const savedDraftId = draftIdRef.current;
        const generation = saveGeneration.current;
        if (savedDraftId) {
          // Sending a new reply and deleting its draft must not bypass the MIME/revision guard.
          const updated = await updateDraft.mutateAsync({
            params: { path: { id: savedDraftId } },
            body: {
              accountId: plan.accountId,
              to: reviewed.input.to,
              cc: reviewed.input.cc,
              subject: "",
              body: reviewed.input.body,
              replyToEntry: reviewed.input.entry,
              replyAll: reviewed.input.all,
              expectedMessageId: draftMessageIdRef.current,
            },
          });
          if (
            !reviewInput.isCurrent(reviewed) ||
            generation !== saveGeneration.current ||
            savedDraftId !== draftIdRef.current
          )
            return;
          draftMessageIdRef.current = updated.messageId ?? "";
          setDraftSaveError("");
        }
        sending = true;
        await send.mutateAsync({
          body: { ...reviewed.input, confirm: true, accountId: plan.accountId },
        });
        let draftCleanupFailed = false;
        if (savedDraftId) {
          ownership.retire(plan.accountId ?? displayedAccountId, savedDraftId);
          selected.coordinator?.retire(plan.accountId ?? displayedAccountId, savedDraftId);
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
        // Only a send attempt has an uncertain delivery outcome; preflight failures sent nothing.
        if (sending) dispatch({ type: "sendFailed" });
        return sending
          ? refusal(e, "-send-mail", "Sending the reply")
          : refusal(e, "-draft-mail", "Checking the Gmail reply draft");
      },
    );
    staleAfterMail(queryClient);
  }

  async function discardDraft() {
    if (scopePendingRef.current) return;
    const id = draftIdRef.current;
    if (!id || !window.confirm("Discard this Gmail reply draft?")) return;
    const ownerGeneration = saveGeneration.current;
    await run(async () => {
      await saveQueue.current;
      if (ownerGeneration !== saveGeneration.current || id !== draftIdRef.current) return;
      const generation = ++saveGeneration.current;
      await deleteDraft.mutateAsync({
        params: { path: { id }, query: { accountId: displayedAccountId } },
      });
      ownership.retire(displayedAccountId, id);
      selected.coordinator?.retire(displayedAccountId, id);
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

  const errorAlert =
    error && !scopePending ? (
      <InlineAlert>
        {error}
        {sendFailed ? " Check Gmail before attempting to send again." : ""}
      </InlineAlert>
    ) : null;

  return (
    <div className="mt-4 mb-1 rounded-lg border border-line bg-card px-3 py-2" ref={host}>
      {!scopePending && drafts.isError ? (
        <DraftReadError
          error={drafts.error}
          retrying={drafts.isFetching}
          onRetry={() => void drafts.refetch()}
        />
      ) : null}
      {!scopePending && draftSaveError ? (
        <DraftSaveError error={draftSaveError} draft={recoveryDraft} />
      ) : null}
      {!scopePending ? (
        <DraftCompatibilityNotice check={selected.preflight} draft={recoveryDraft} />
      ) : null}
      <section aria-label="Reply" aria-busy={selected.preflight.pending || undefined}>
        {step.kind === "reviewing" ? (
          <>
            <div
              className={scopePending ? "hidden" : undefined}
              aria-hidden={scopePending || undefined}
            >
              <ReplyPlan plan={step.plan} />
            </div>
            {errorAlert}
            <footer className="mt-2 flex flex-wrap items-center justify-end gap-2">
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
                  disabled={
                    scopePending || busy || selected.blocked || !previewIsCurrent || to.length === 0
                  }
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
              hideAudience={scopePending}
              mode={{
                kind: "reply",
                editingRecipients,
                onEditRecipients: () => dispatch({ type: "editRecipients" }),
                target: <ReplyTarget answerAnchor={answerAnchor} words={words} />,
              }}
              from={
                <AccountSelect
                  value={displayedAccountId}
                  disabled={busy || selected.editingBlocked}
                  onChange={(id) => {
                    if (selected.coordinator && !selected.coordinator.detach(id)) return;
                    continuityAccount.current = id;
                    transferAccount.current = id;
                    selected.edit();
                    ownership.edit();
                    reviewInput.invalidate();
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
              busy={busy || selected.editingBlocked}
            />
            <footer className="mt-2 flex flex-wrap items-center justify-end gap-2">
              <div className="mr-auto flex flex-wrap items-center gap-4">
                <CheckboxRow
                  className="flex cursor-pointer items-center gap-1.5 text-xs text-muted has-disabled:cursor-default has-disabled:opacity-55"
                  title="Answer everyone the message was addressed to. Edit the reply's audience in the fields above."
                  checked={all}
                  disabled={busy || selected.editingBlocked}
                  accent="accent"
                  inputClassName="m-0 cursor-pointer"
                  onChange={(e) => {
                    selected.edit();
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
                  disabled={busy || selected.editingBlocked}
                  accent="accent"
                  inputClassName="m-0 cursor-pointer"
                  onChange={(e) => {
                    selected.edit();
                    dispatch({ type: "html", html: e.target.checked });
                  }}
                >
                  send html
                </CheckboxRow>
                <BodyWordCount body={own} />
              </div>
              {draftId ? (
                <Button
                  variant="danger"
                  density="compact"
                  type="button"
                  disabled={scopePending || busy}
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
                disabled={
                  scopePending ||
                  busy ||
                  selected.blocked ||
                  own.trim() === "" ||
                  (recipients.toTouched && to.length === 0)
                }
                onClick={() => void review()}
              >
                {busy ? "Preparing…" : "preview"}
              </Button>
            </footer>
          </>
        )}
      </section>
      {selected.replacement ? (
        <InlineAlert>
          New input arrived while the selected draft was loading.{" "}
          <Button type="button" density="compact" onClick={selected.replace}>
            Replace with selected draft
          </Button>
        </InlineAlert>
      ) : null}
      {!scopePending ? (
        <DraftCards
          accountId={displayedAccountId}
          editingId={draftId || undefined}
          drafts={(drafts.data ?? []).filter(
            (item) =>
              item.kind === "reply" &&
              item.accountId === displayedAccountId &&
              draftDestination(item) === thread.rootExtId &&
              !selected.coordinator?.isRetired(item.accountId, item.id),
          )}
          loading={drafts.isPending}
          onSelect={(item) => selected.coordinator?.select(item)}
        />
      ) : null}
    </div>
  );
}
