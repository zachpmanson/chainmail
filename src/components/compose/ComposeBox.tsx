import { useEffect, useReducer, useRef, useState, type FormEvent } from "react";
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
import ComposePreview from "./ComposePreview";
import { emptyDraft, parseAddressHeader, type Draft } from "./Draft";
import SelectInput from "../ui/SelectInput";
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
  const { busy, error, clearError, run } = useBusyTask();
  const [draftId, setDraftId] = useState("");
  const [draftSaveError, setDraftSaveError] = useState("");
  const draftIdRef = useRef("");
  const draftMessageIdRef = useRef("");
  const saveGeneration = useRef(0);
  const saveQueue = useRef(Promise.resolve());
  const displayedAccountId = accountId || (connected.length === 1 ? (connected[0]?.id ?? "") : "");
  const drafts = $api.useQuery(
    "get",
    "/v1/drafts",
    {
      params: { query: displayedAccountId ? { accountId: displayedAccountId } : {} },
    },
    { enabled: Boolean(displayedAccountId) },
  );
  const to = draft.to.map(addressWords);
  const cc = draft.cc.map(addressWords);
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

  useEffect(() => {
    if (
      (!draft.subject.trim() && !draft.body.trim() && !draft.to.length && !draft.cc.length) ||
      !displayedAccountId ||
      drafts.isPending ||
      drafts.isError ||
      draftSaveError ||
      busy ||
      step.kind !== "editing"
    )
      return;
    const content = { to, cc, subject: draft.subject, body: draft.body };
    const generation = saveGeneration.current;
    const timer = window.setTimeout(() => {
      saveQueue.current = saveQueue.current
        .catch(() => undefined)
        .then(async () => {
          if (generation !== saveGeneration.current) return;
          const id = draftIdRef.current;
          const body = {
            accountId: displayedAccountId,
            ...content,
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
        .catch((e) => {
          console.error("Saving Gmail draft failed", e);
          if (generation !== saveGeneration.current) return;
          setDraftSaveError(
            `${errText(e)} Autosave stopped; check Gmail before resuming this draft.`,
          );
        });
    }, 700);
    return () => window.clearTimeout(timer);
  }, [
    draft,
    draftSaveError,
    busy,
    to,
    cc,
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
    await run(async () => {
      await saveQueue.current;
      const generation = ++saveGeneration.current;
      await deleteDraft.mutateAsync({
        params: { path: { id }, query: { accountId: displayedAccountId } },
      });
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
    const reviewed = reviewInput.capture();
    await run(async () => {
      const preview = await compose.mutateAsync({ body: { ...reviewed.input, confirm: false } });
      if (reviewInput.isCurrent(reviewed)) dispatch({ type: "previewed", preview, reviewed });
    }, errText);
  }

  async function send() {
    if (step.kind !== "reviewing" || !previewIsCurrent) return;
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
                    disabled={busy || !previewIsCurrent}
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
            {draftSaveError ? <InlineAlert>{draftSaveError}</InlineAlert> : null}
            {drafts.data?.length ? (
              <label className="mb-2 flex items-center gap-2 text-xs text-muted">
                Resume Gmail draft
                <SelectInput
                  value=""
                  disabled={busy}
                  onChange={(event) => {
                    const selected = drafts.data?.find((item) => item.id === event.target.value);
                    if (!selected) return;
                    saveGeneration.current++;
                    setDraftSaveError("");
                    draftIdRef.current = selected.id;
                    draftMessageIdRef.current = selected.messageId ?? "";
                    setDraftId(selected.id);
                    if (selected.accountId !== accountId)
                      dispatch({ type: "account", accountId: selected.accountId });
                    dispatch({
                      type: "draft",
                      patch: {
                        to: parseAddressHeader(selected.to),
                        cc: parseAddressHeader(selected.cc),
                        subject: selected.subject ?? "",
                        body: selected.body ?? "",
                      },
                    });
                  }}
                >
                  <option value="">Choose a draft…</option>
                  {drafts.data.map((item) => (
                    <option key={`${item.accountId}:${item.id}`} value={item.id}>
                      {item.subject || item.body?.slice(0, 70) || "Untitled draft"}
                    </option>
                  ))}
                </SelectInput>
              </label>
            ) : null}
            {draftId ? (
              <div className="mb-1 flex items-center justify-between">
                <p className="text-2xs text-muted">Saved to Gmail</p>
                <Button
                  variant="danger"
                  density="compact"
                  type="button"
                  disabled={busy}
                  onClick={() => void discardDraft()}
                >
                  Discard draft
                </Button>
              </div>
            ) : null}
            <ComposeForm
              draft={draft}
              onDraft={(patch) => dispatch({ type: "draft", patch })}
              accountId={accountId}
              onAccount={(id) => {
                saveGeneration.current++;
                setDraftSaveError("");
                draftIdRef.current = "";
                draftMessageIdRef.current = "";
                setDraftId("");
                dispatch({ type: "account", accountId: id });
              }}
              busy={busy}
              onSubmit={(event) => void prepare(event)}
              onClose={onClose}
            />
          </>
        )}
      </div>
    </aside>
  );
}
