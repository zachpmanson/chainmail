import type { OpsMerge } from "../../lib/api/api";
import Button from "../ui/Button";

export default function MergeConfirm({
  chosen,
  onConfirm,
  onCancel,
}: {
  chosen: OpsMerge[];
  onConfirm: () => void;
  onCancel: () => void;
}) {
  return (
    <div className="mt-2 mb-0.5 rounded-md border border-line bg-quote p-2 text-xs/normal text-fg">
      <p className="m-0">
        <strong>This cannot be undone</strong> — a merge is recorded, never reversed.{" "}
        {chosen.length === 1 ? "This pair" : `These ${chosen.length} pairs`} will be folded into
        their keepers now:
      </p>
      <ul className="mt-1.5 mb-0 list-none pl-0.5">
        {chosen.map((m) => (
          <li
            key={m.dropId}
            className="py-px text-xs/normal [&_code]:text-xs [&_code]:wrap-anywhere"
          >
            <code>
              #{m.dropId} {m.dropName}
            </code>{" "}
            <span className="text-muted">→</span>{" "}
            <code>
              #{m.keepId} {m.keepName}
            </code>
          </li>
        ))}
      </ul>
      <div className="mt-2 flex items-center gap-2">
        <Button type="button" variant="danger" density="compact" onClick={onConfirm}>
          {chosen.length === 1 ? "merge this pair" : `merge these ${chosen.length} pairs`}
        </Button>
        <Button type="button" variant="secondary" density="compact" onClick={onCancel}>
          cancel
        </Button>
      </div>
    </div>
  );
}
