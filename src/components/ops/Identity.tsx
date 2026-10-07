import type { PersonSummary } from "../../lib/api/api";
import { Button } from "../ui/controls";

export default function Identity({
  id,
  owner,
  busy,
  onDetach,
}: {
  id: string;
  owner: PersonSummary;
  busy: boolean;
  onDetach: (identity: string) => void;
}) {
  return (
    <span className="mr-1 mb-1 inline-flex items-center gap-0.5 rounded-md border border-line bg-card px-1 py-px pr-1">
      <code className="text-xs wrap-anywhere">{id}</code>
      <Button
        type="button"
        variant="quiet"
        className="size-auto! min-h-0! rounded px-1 py-0 text-sm leading-none text-muted hover:bg-quote hover:text-red-700 disabled:opacity-40"
        disabled={busy}
        title={`detach ${id}`}
        aria-label={`Detach ${id} from ${owner.displayName}`}
        onClick={() => onDetach(id)}
      >
        ×
      </Button>
    </span>
  );
}
