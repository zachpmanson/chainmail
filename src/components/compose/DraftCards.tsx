import { draftDestination, type SavedDraft } from "../../lib/ui/draftSelection";
import Button from "../ui/Button";

export default function DraftCards({
  drafts,
  loading,
  selected,
  onSelect,
}: {
  drafts: SavedDraft[];
  loading: boolean;
  selected?: string;
  onSelect: (draft: SavedDraft) => void;
}) {
  return (
    <section aria-label="Saved Gmail drafts" className="mt-3 border-t border-line pt-2">
      <h3 className="my-1 text-xs font-semibold">Saved Gmail drafts</h3>
      {loading ? (
        <p className="text-xs text-muted" role="status">
          Loading drafts…
        </p>
      ) : !drafts.length ? (
        <p className="text-xs text-muted">No saved drafts.</p>
      ) : (
        <ul className="m-0 grid list-none gap-2 p-0">
          {drafts.map((draft) => (
            <li
              key={JSON.stringify([draft.accountId, draft.id])}
              className="rounded-md border border-line p-2"
              data-draft={draft.id}
            >
              <p className="m-0 text-2xs text-muted">
                {draft.kind === "reply"
                  ? "Reply"
                  : draft.kind === "compose"
                    ? "Compose"
                    : "Unresolved"}
                {selected === draft.id ? " · Selected" : ""}
              </p>
              <p className="my-1 text-xs font-semibold wrap-break-word">
                {draft.subject || "Untitled draft"}
              </p>
              <p className="my-1 text-xs wrap-break-word">{draft.body?.slice(0, 140)}</p>
              {draftDestination(draft) ? (
                <Button type="button" density="compact" onClick={() => onSelect(draft)}>
                  Open draft
                </Button>
              ) : (
                <p className="my-1 text-2xs text-muted">
                  Cannot open: {draft.resolution || "destination unavailable"}.
                </p>
              )}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
