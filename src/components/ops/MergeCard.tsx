import type { OpsMerge } from "../../lib/api/api";
import { ruleLabel } from "../../lib/ops/opsMerges";
import Checkbox from "../ui/Checkbox";
import StatusBadge from "../ui/StatusBadge";

/** The server decides applicability; the checkbox only mirrors it. */
export default function MergeCard({
  m,
  selected,
  busy,
  onSelect,
}: {
  m: OpsMerge;
  selected: boolean;
  busy: boolean;
  onSelect: (picked: boolean) => void;
}) {
  const keep =
    `#${m.keepId} ${m.keepName}` +
    (m.keepIdentities?.length ? ` · ${m.keepIdentities.join(", ")}` : "");
  const drop =
    `#${m.dropId} ${m.dropName}` +
    (m.dropIdentities?.length ? ` · ${m.dropIdentities.join(", ")}` : "");
  return (
    <article>
      <p className="mt-0 mb-1.5 text-2xs font-bold tracking-[.05em] text-muted uppercase">
        {m.applicable ? (
          <Checkbox
            className="mr-2 cursor-pointer align-middle"
            checked={selected}
            disabled={busy}
            onChange={(e) => onSelect(e.target.checked)}
            aria-label={`Select folding #${m.dropId} ${m.dropName} into #${m.keepId} ${m.keepName}`}
          />
        ) : null}
        {ruleLabel(m.rule)}
        {m.applicable ? (
          <StatusBadge className="ml-2" tone="success">
            apply
          </StatusBadge>
        ) : (
          <StatusBadge tone="neutral">read-only</StatusBadge>
        )}
      </p>
      <p className="my-0.5 text-sm/snug [&_code]:text-xs [&_code]:wrap-anywhere">
        keep&nbsp;<code>{keep}</code>
      </p>
      <p className="my-0.5 text-sm/snug [&_code]:text-xs [&_code]:wrap-anywhere">
        drop&nbsp;<code>{drop}</code>
      </p>
      {m.evidence ? <p className="my-1 mb-2 text-xs text-muted">{m.evidence}.</p> : null}
    </article>
  );
}
