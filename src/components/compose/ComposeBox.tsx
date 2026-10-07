import { useReducer, type FormEvent } from "react";
import { $api, type ComposeResponse } from "../../lib/api/api";
import { useAccountId } from "../../lib/inbox/useAccountId";
import { errText } from "../../lib/ui/errText";
import { addressWords } from "./AddressField";
import Button from "../ui/Button";
import InlineAlert from "../ui/InlineAlert";
import ComposeDone from "./ComposeDone";
import ComposeForm from "./ComposeForm";
import ComposePreview from "./ComposePreview";
import { emptyDraft, type Draft } from "./Draft";
import useBusyTask from "./useBusyTask";

type Step =
  | { kind: "editing" }
  | { kind: "reviewing"; preview: ComposeResponse; sendFailed: boolean }
  | { kind: "sent"; result: ComposeResponse };

type State = { draft: Draft; accountId: string; step: Step };

type Action =
  | { type: "draft"; patch: Partial<Draft> }
  | { type: "account"; accountId: string }
  | { type: "previewed"; preview: ComposeResponse }
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
      return { ...state, step: { kind: "reviewing", preview: action.preview, sendFailed: false } };
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
  const routeAccountId = useAccountId();
  const [{ draft, accountId, step }, dispatch] = useReducer(reduce, {
    draft: emptyDraft,
    accountId: routeAccountId ?? "",
    step: { kind: "editing" },
  });
  const { busy, error, clearError, run } = useBusyTask();
  const to = draft.to.map(addressWords);
  const cc = draft.cc.map(addressWords);
  const sendFailed = step.kind === "reviewing" && step.sendFailed;
  // Sending is only allowed for exactly what the reader reviewed.
  const previewIsCurrent =
    step.kind === "reviewing" &&
    step.preview.to === to.join(", ") &&
    step.preview.cc === (cc.length ? cc.join(", ") : undefined) &&
    step.preview.subject === draft.subject &&
    step.preview.body === draft.body;

  async function prepare(event: FormEvent) {
    event.preventDefault();
    await run(async () => {
      const preview = await compose.mutateAsync({
        body: {
          to,
          cc,
          subject: draft.subject,
          body: draft.body,
          confirm: false,
          ...(accountId ? { accountId } : {}),
        },
      });
      dispatch({ type: "previewed", preview });
    }, errText);
  }

  async function send() {
    if (step.kind !== "reviewing" || !previewIsCurrent) return;
    const { preview } = step;
    await run(
      async () => {
        const result = await compose.mutateAsync({
          body: {
            to,
            cc,
            subject: draft.subject,
            body: draft.body,
            confirm: true,
            accountId: preview.accountId,
          },
        });
        dispatch({ type: "sent", result });
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
            <ComposeForm
              draft={draft}
              onDraft={(patch) => dispatch({ type: "draft", patch })}
              accountId={accountId}
              onAccount={(id) => dispatch({ type: "account", accountId: id })}
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
