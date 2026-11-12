import DraftSaveError from "./DraftSaveError";
import DraftCompatibilityNotice from "./DraftCompatibilityNotice";
import DraftCards from "./DraftCards";
import useSelectedDraft from "./useSelectedDraft";
import { draftIdentity } from "../../lib/ui/draftSelection";
import { useEffect, useMemo, useReducer, useRef, useState, type FormEvent } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { $api, type ComposeResponse } from "../../lib/api/api";
import { useAccountId } from "../../lib/inbox/useAccountId";
import { errText } from "../../lib/ui/errText";
import ReviewInput, { type ReviewedInput } from "../../lib/message/reviewedInput";
import type { components } from "../../lib/api/api.d";
import { addressWords } from "./AddressField";
import Button from "../ui/Button";
import InlineAlert from "../ui/InlineAlert";
import ComposeDone from "./ComposeDone";
import ComposeForm from "./ComposeForm";
import DraftReadError from "./DraftReadError";
import ComposePreview from "./ComposePreview";
import { emptyDraft, isEmptyDraft, parseAddressHeader, type Draft } from "./Draft";

import useAccounts from "./useAccounts";
import useBusyTask from "./useBusyTask";

type ComposeInput = Omit<components["schemas"]["ComposeRequest"], "confirm">;

type Step =
  | { kind: "editing" }
  | {
      kind: "reviewing";
      preview: ComposeResponse;
      reviewed: ReviewedInput<ComposeInput>;
      sendFailed: boolean;
    }
  | { kind: "sent"; result: ComposeResponse };

type State = { draft: Draft; accountId: string; step: Step };

type Action =
  | { type: "draft"; patch: Partial<Draft> }
  | { type: "account"; accountId: string }
  | { type: "previewed"; preview: ComposeResponse; reviewed: ReviewedInput<ComposeInput> }
  | { type: "edit" }
  | { type: "sent"; result: ComposeResponse }
  | { type: "sendFailed" };

function reduce(state: State, action: Action): State {
  switch (action.type) {
    case "draft":
      return { ...state, draft: { ...state.draft, ...action.patch } };
    case "account":
      return { ...state, accountId: action.accountId, step: { kind: "editing" } };
    case "previewed":
      return {
        ...state,
        step: {
          kind: "reviewing",
          preview: action.preview,
          reviewed: action.reviewed,
          sendFailed: false,
        },
      };
    case "edit":
      return { ...state, step: { kind: "editing" } };
    case "sent":
      return { ...state, step: { kind: "sent", result: action.result } };
    case "sendFailed":
      return state.step.kind === "reviewing"
        ? { ...state, step: { ...state.step, sendFailed: true } }
        : state;
  }
}

export default function ComposeBox({ onClose }: { onClose: () => void }) {
  const compose = $api.useMutation("post", "/v1/compose");
  const createDraft = $api.useMutation("post", "/v1/drafts");
  const updateDraft = $api.useMutation("put", "/v1/drafts/{id}");
  const sendDraft = $api.useMutation("post", "/v1/drafts/{id}/send");
  const deleteDraft = $api.useMutation("delete", "/v1/drafts/{id}");
  const routeAccountId = useAccountId();
  const { connected } = useAccounts();
  const queryClient = useQueryClient();
  const [{ draft, accountId, step }, dispatch] = useReducer(reduce, {
    draft: emptyDraft,
    accountId: routeAccountId ?? "",
    step: { kind: "editing" },
  });
  const { busy, busyRef, operationRevision, error, clearError, run } = useBusyTask();
  const latestDraft = useRef(draft);
  latestDraft.current = draft;
  const [draftId, setDraftId] = useState("");
  const [draftSaveError, setDraftSaveError] = useState("");
  const acknowledgedSaveRef = useRef("");
  const draftIdRef = useRef("");
  const draftMessageIdRef = useRef("");
  const saveGeneration = useRef(0);
  const saveQueue = useRef(Promise.resolve());
  const pendingWrites = useRef(0);
  const [pendingWriteCount, setPendingWriteCount] = useState(0);
  const writeRevision = useRef(0);
  const displayedAccountId = accountId || (connected.length === 1 ? (connected[0]?.id ?? "") : "");
  const drafts = $api.useQuery(
    "get",
    "/v1/drafts",
    {
      params: { query: displayedAccountId ? { accountId: displayedAccountId } : {} },
    },
    { enabled: Boolean(displayedAccountId) },
  );
  const draftInput = useMemo(
    () => ({
      accountId: displayedAccountId,
      to: draft.to.map(addressWords),
      cc: draft.cc.map(addressWords),
      subject: draft.subject,
      body: draft.body,
    }),
    [displayedAccountId, draft],
  );
  const { to, cc } = draftInput;
  const draftInputKey = JSON.stringify(draftInput);
  const sendFailed = step.kind === "reviewing" && step.sendFailed;
  const reviewInput = useRef(new ReviewInput<ComposeInput>()).current;
  reviewInput.update({
    to,
    cc,
    subject: draft.subject,
    body: draft.body,
    ...(displayedAccountId ? { accountId: displayedAccountId } : {}),
  });
  // Server headers may be canonicalised (for example RFC 2047 display names).
  const previewIsCurrent = step.kind === "reviewing" && reviewInput.isCurrent(step.reviewed);
  useEffect(
    () => () => {
      reviewInput.invalidate();
      saveGeneration.current++;
    },
    [reviewInput],
  );

  const selected = useSelectedDraft({
    kind: "compose",
    account: displayedAccountId,
    id: draftId,
    snapshot: () => ({
      kind: "compose",
      destination: "compose",

      revision: JSON.stringify([
        draftInputKey,
        step.kind,
        draftSaveError,
        sendFailed,
        operationRevision.current,
        writeRevision.current,
      ]),
      owner: draftIdRef.current ? draftIdentity(displayedAccountId, draftIdRef.current) : undefined,
      busy:
        busyRef.current ||
        pendingWrites.current > 0 ||
        createDraft.isPending ||
        updateDraft.isPending ||
        deleteDraft.isPending,
      dirty:
        step.kind !== "sent" &&
        Boolean(
          draft.subject ||
          draft.body ||
          draft.to.length ||
          draft.cc.length ||
          draftSaveError ||
          sendFailed,
        ),
      invalidate: () => {
        saveGeneration.current++;
        reviewInput.invalidate();
      },
    }),
    hydrate: (saved) => {
      saveGeneration.current++;
      reviewInput.invalidate();
      dispatch({ type: "edit" });
      setDraftSaveError("");
      draftIdRef.current = saved.id;
      draftMessageIdRef.current = saved.messageId ?? "";
      setDraftId(saved.id);
      const to = parseAddressHeader(saved.to);
      const cc = parseAddressHeader(saved.cc);
      dispatch({ type: "account", accountId: saved.accountId });
      dispatch({
        type: "draft",
        patch: { to, cc, subject: saved.subject ?? "", body: saved.body ?? "" },
      });
      const input = {
        accountId: saved.accountId,
        to: to.map(addressWords),
        cc: cc.map(addressWords),
        subject: saved.subject ?? "",
        body: saved.body ?? "",
      };
      const acknowledgement = JSON.stringify([
        saveGeneration.current,
        saved.id,
        JSON.stringify(input),
      ]);
      acknowledgedSaveRef.current = acknowledgement;
    },
  });

  const autosaveAllowed = Boolean(
    displayedAccountId &&
    !selected.blocked &&
    !drafts.isPending &&
    !drafts.isError &&
    !draftSaveError &&
    !busy &&
    step.kind === "editing",
  );
  const autosaveScope = useRef({ account: displayedAccountId, allowed: autosaveAllowed });
  autosaveScope.current = { account: displayedAccountId, allowed: autosaveAllowed };

  useEffect(() => {
    if (!autosaveAllowed || (isEmptyDraft(draft) && !draftIdRef.current)) return;
    const generation = saveGeneration.current;
    if (
      acknowledgedSaveRef.current ===
      JSON.stringify([generation, draftIdRef.current, draftInputKey])
    )
      return;
    const timer = window.setTimeout(() => {
      writeRevision.current++;
      pendingWrites.current++;
      setPendingWriteCount(pendingWrites.current);
      saveQueue.current = saveQueue.current
        .catch(() => undefined)
        .then(async () => {
          if (
            generation !== saveGeneration.current ||
            autosaveScope.current.account !== displayedAccountId ||
            !autosaveScope.current.allowed ||
            busyRef.current
          )
            return;
          const id = draftIdRef.current;
          // The debounce may have queued this create just before a synchronous clear.
          if (!id && isEmptyDraft(latestDraft.current)) return;
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
        .catch((e) => {
          console.error("Saving Gmail draft failed", e);
          if (generation !== saveGeneration.current) return;
          // Cancel work already queued behind this failed write, before React renders the error.
          saveGeneration.current++;
          setDraftSaveError(
            `${errText(e)} Autosave stopped; check Gmail before resuming this draft.`,
          );
        })
        .finally(() => {
          pendingWrites.current--;
          setPendingWriteCount(pendingWrites.current);
        });
    }, 700);
    return () => window.clearTimeout(timer);
  }, [
    draft,
    draftId,
    autosaveAllowed,
    busyRef,
    selected.blocked,
    draftSaveError,
    busy,
    draftInput,
    draftInputKey,
    displayedAccountId,
    drafts.isPending,
    drafts.isError,
    step.kind,
    createDraft,
    updateDraft,
    queryClient,
  ]);

  async function discardDraft() {
    const id = draftIdRef.current;
    if (!id || !window.confirm("Discard this Gmail draft?")) return;
    const ownerGeneration = saveGeneration.current;
    await run(async () => {
      await saveQueue.current;
      if (ownerGeneration !== saveGeneration.current || id !== draftIdRef.current) return;
      const generation = ++saveGeneration.current;
      await deleteDraft.mutateAsync({
        params: { path: { id }, query: { accountId: displayedAccountId } },
      });
      selected.coordinator?.retire(displayedAccountId, id);
      if (generation !== saveGeneration.current) return;
      draftIdRef.current = "";
      draftMessageIdRef.current = "";
      setDraftId("");
      setDraftSaveError("");
      dispatch({ type: "draft", patch: emptyDraft });
      await queryClient.invalidateQueries({ queryKey: ["get", "/v1/drafts"] });
    }, errText);
  }

  async function prepare(event: FormEvent) {
    event.preventDefault();
    if (selected.blocked) return;
    const reviewed = reviewInput.capture();
    await run(async () => {
      const preview = await compose.mutateAsync({ body: { ...reviewed.input, confirm: false } });
      if (reviewInput.isCurrent(reviewed)) dispatch({ type: "previewed", preview, reviewed });
    }, errText);
  }

  async function send() {
    if (selected.blocked || step.kind !== "reviewing" || !previewIsCurrent) return;
    const { preview, reviewed } = step;
    const input = reviewed.input;
    await run(
      async () => {
        await saveQueue.current;
        saveGeneration.current++;
        if (!reviewInput.isCurrent(reviewed)) return;
        let result: ComposeResponse;
        if (draftIdRef.current) {
          const id = draftIdRef.current;
          const account = preview.accountId ?? displayedAccountId;
          const updated = await updateDraft.mutateAsync({
            params: { path: { id } },
            body: {
              ...input,
              accountId: account,
              expectedMessageId: draftMessageIdRef.current,
            },
          });
          draftMessageIdRef.current = updated.messageId ?? "";
          if (!reviewInput.isCurrent(reviewed)) return;
          const sent = await sendDraft.mutateAsync({
            params: { path: { id } },
            body: { accountId: account },
          });
          result = {
            accountId: account,
            to: input.to.join(", "),
            cc: input.cc?.join(", "),
            subject: input.subject,
            body: input.body,
            sent: sent.sent,
            gmailId: sent.gmailId,
            filed: sent.filed,
          };
          selected.coordinator?.retire(account, id);
          if (reviewInput.isCurrent(reviewed)) {
            draftIdRef.current = "";
            draftMessageIdRef.current = "";
            setDraftId("");
          }
          await queryClient.invalidateQueries({ queryKey: ["get", "/v1/drafts"] });
        } else {
          result = await compose.mutateAsync({
            body: {
              ...input,
              confirm: true,
              accountId: preview.accountId,
            },
          });
        }
        if (reviewInput.isCurrent(reviewed)) dispatch({ type: "sent", result });
      },
      (e) => {
        // The request may have reached Gmail even if its response did not; a retry could duplicate mail.
        dispatch({ type: "sendFailed" });
        return errText(e);
      },
    );
  }

  const errorAlert = error ? (
    <InlineAlert>
      {error}
      {sendFailed ? " Check Gmail before attempting to send again." : ""}
    </InlineAlert>
  ) : null;

  return (
    <aside
      className="ibread flex min-w-0 flex-col overflow-auto bg-bg min-[60rem]:h-full min-[60rem]:min-h-0 min-[60rem]:px-5"
      aria-label="Compose email"
    >
      <div className="mt-4 mb-1 rounded-lg border border-line bg-card px-3 py-2">
        {step.kind === "sent" ? (
          <ComposeDone result={step.result} onClose={onClose} />
        ) : step.kind === "reviewing" ? (
          <>
            <ComposePreview preview={step.preview} />
            {errorAlert}
            <footer className="mt-2 flex items-center justify-end gap-2">
              {sendFailed ? (
                <Button variant="subtle" density="compact" type="button" onClick={onClose}>
                  Close
                </Button>
              ) : (
                <>
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
                    Edit
                  </Button>
                  <Button
                    variant="danger"
                    density="compact"
                    type="button"
                    disabled={busy || selected.blocked || !previewIsCurrent}
                    onClick={() => void send()}
                  >
                    {busy ? "Sending…" : "Confirm and send"}
                  </Button>
                </>
              )}
            </footer>
          </>
        ) : (
          <>
            {errorAlert}
            {drafts.isError ? (
              <DraftReadError
                error={drafts.error}
                retrying={drafts.isFetching}
                onRetry={() => void drafts.refetch()}
              />
            ) : null}
            {draftSaveError ? <DraftSaveError error={draftSaveError} draft={draftInput} /> : null}
            <DraftCompatibilityNotice check={selected.preflight} draft={draftInput} />
            <ComposeForm
              draft={draft}
              onDraft={(patch) => {
                latestDraft.current = { ...latestDraft.current, ...patch };
                selected.edit();
                dispatch({ type: "draft", patch });
              }}
              accountId={accountId}
              onAccount={(id) => {
                if (selected.coordinator && !selected.coordinator.detach(id)) return;
                selected.edit();
                reviewInput.invalidate();
                saveGeneration.current++;
                setDraftSaveError("");
                draftIdRef.current = "";
                draftMessageIdRef.current = "";
                setDraftId("");
                dispatch({ type: "account", accountId: id });
              }}
              busy={busy}
              onSubmit={(event) => void prepare(event)}
              cancelDisabled={pendingWriteCount > 0}
              editingBlocked={selected.editingBlocked}
              checkingDraft={selected.preflight.pending}
              onClose={() => {
                if (busyRef.current || pendingWrites.current > 0) return;
                onClose();
              }}
              onDiscard={draftId ? () => void discardDraft() : undefined}
            />
          </>
        )}
      </div>
      {selected.replacement ? (
        <InlineAlert>
          New input arrived while the selected draft was loading.{" "}
          <Button type="button" density="compact" onClick={selected.replace}>
            Replace with selected draft
          </Button>
        </InlineAlert>
      ) : null}
      <DraftCards
        accountId={displayedAccountId}
        editingId={draftId || undefined}
        drafts={(drafts.data ?? []).filter(
          (item) => !selected.coordinator?.isRetired(item.accountId, item.id),
        )}
        loading={drafts.isPending}
        onSelect={(item) => selected.coordinator?.select(item)}
      />
    </aside>
  );
}
