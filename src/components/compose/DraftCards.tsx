import { draftDestination, type SavedDraft } from "../../lib/ui/draftSelection";
import Button from "../ui/Button";
import { useDraftCoordinator } from "./DraftSelectionContext";

export default function DraftCards({
  accountId,
  drafts,
  loading,
  onSelect,
}: {
  accountId: string;
  drafts: SavedDraft[];
  loading: boolean;
  onSelect: (draft: SavedDraft) => void;
}) {
  const coordinator = useDraftCoordinator();
  const selection = coordinator?.selection;
  const selected =
    selection?.active && selection.accountId === accountId ? selection.id : undefined;
  const pending = Boolean(selected && selection?.loading);
  const pendingNotice = (
    <>
      <p className="my-1 text-xs text-muted" role="status">
        Loading selected draft…
      </p>
      <Button type="button" density="compact" onClick={() => coordinator?.clear()}>
        Clear draft selection
      </Button>
    </>
  );
  return (
    <section aria-label="Saved Gmail drafts" className="mt-3 border-t border-line pt-2">
      <h3 className="my-1 text-xs font-semibold">Saved Gmail drafts</h3>
      {loading && !pending ? (
        <p className="text-xs text-muted" role="status">
          Loading drafts…
        </p>
      ) : !loading && !drafts.length && !pending ? (
        <p className="text-xs text-muted">No saved drafts.</p>
      ) : null}
      <ul className="m-0 grid list-none gap-2 p-0">
        {pending &&
        !drafts.some((draft) => draft.accountId === accountId && draft.id === selected) ? (
          <li className="rounded-md border border-line p-2" data-draft={selected} aria-busy="true">
            {pendingNotice}
          </li>
        ) : null}
        {drafts.map((draft) => {
          const isSelected = draft.accountId === accountId && selected === draft.id;
          return (
            <li
              key={JSON.stringify([draft.accountId, draft.id])}
              className="rounded-md border border-line p-2"
              data-draft={draft.id}
              aria-busy={isSelected && pending ? true : undefined}
            >
              <p className="m-0 text-2xs text-muted">
                {draft.kind === "reply"
                  ? "Reply"
                  : draft.kind === "compose"
                    ? "Compose"
                    : "Unresolved"}
                {isSelected ? " · Selected" : ""}
              </p>
              <p className="my-1 text-xs font-semibold wrap-break-word">
                {draft.subject || "Untitled draft"}
              </p>
              <p className="my-1 text-xs wrap-break-word">{draft.body?.slice(0, 140)}</p>
              {isSelected && pending ? (
                pendingNotice
              ) : draftDestination(draft) ? (
                <Button type="button" density="compact" onClick={() => onSelect(draft)}>
                  Open draft
                </Button>
              ) : (
                <p className="my-1 text-2xs text-muted">
                  Cannot open: {draft.resolution || "destination unavailable"}.
                </p>
              )}
            </li>
          );
        })}
      </ul>
    </section>
  );
}
