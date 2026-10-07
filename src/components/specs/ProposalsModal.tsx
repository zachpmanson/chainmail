import { useState } from "react";
import Checkbox from "../ui/Checkbox";
import DialogShell from "../ui/DialogShell";
import { Button } from "../ui/controls";
import ThreadPreview from "../thread/ThreadPreview";
import type { RefreshCandidate } from "../../lib/api/api";

export default function ProposalsModal({
  proposals,
  open,
  refreshing,
  onClose,
  onAccept,
}: {
  proposals: RefreshCandidate[];
  open: boolean;
  refreshing?: boolean;
  onClose: () => void;
  onAccept: (ids: string[]) => void;
}) {
  const [accepted, setAccepted] = useState<Set<string>>(new Set());
  const [preview, setPreview] = useState<RefreshCandidate | null>(null);
  if (!open) return null;

  const ids = proposals.map((p) => p.rootExtId);
  const toggle = (root: string) =>
    setAccepted((prev) => {
      const next = new Set(prev);
      if (next.has(root)) next.delete(root);
      else next.add(root);
      return next;
    });

  return (
    <>
      <DialogShell label="Proposed chains">
        <div className="flex items-center gap-2 border-b border-line py-2 px-3">
          <b className="text-xs font-bold uppercase tracking-[.09em] text-muted">proposed</b>
          <span className="ml-auto text-xs text-muted">
            found by a query, not yet on the page — accept the ones that belong
          </span>
        </div>
        <ul className="m-0 flex list-none flex-col gap-1.5 overflow-auto py-2 px-3">
          {proposals.map((p) => {
            const on = accepted.has(p.rootExtId);
            return (
              <li
                key={p.subject ?? p.container ?? p.rootExtId}
                className="flex flex-row items-center gap-2 rounded-md border border-line bg-bg p-2"
              >
                <div className="flex min-w-0 flex-1 flex-col gap-1">
                  <label className="flex min-w-0 flex-1 cursor-pointer items-start gap-2">
                    <Checkbox
                      className="mt-1"
                      accent="org"
                      checked={on}
                      onChange={() => toggle(p.rootExtId)}
                    />
                    <span className="max-w-full wrap-break-word whitespace-normal font-(family-name:--serif) font-semibold">
                      {p.subject ?? <em className="not-italic text-muted">no subject</em>}
                    </span>
                  </label>
                  <span className="mt-0.5 text-xs text-muted">
                    {p.matched}/{p.entries} matched · {p.span ?? ""} · {p.query}
                    {p.semantic
                      ? ` · sim ${p.similarity?.toFixed(2) ?? "–"}${p.lexical ? " (hybrid)" : " (semantic)"}`
                      : p.lexical
                        ? " · word match"
                        : ""}
                  </span>
                  <code className="mt-0.5 max-w-full wrap-break-word text-2xs text-accent">
                    {p.rootExtId}
                  </code>
                </div>
                <Button
                  type="button"
                  className="mr-0 shrink-0 whitespace-nowrap px-2 py-1 text-xs font-semibold text-muted hover:border-accent hover:text-accent"
                  aria-haspopup="dialog"
                  onClick={() => setPreview(p)}
                >
                  Preview
                </Button>
              </li>
            );
          })}
        </ul>
        <div className="flex justify-end gap-2 border-t border-line py-2 px-3">
          <Button
            type="button"
            disabled={refreshing || accepted.size === 0}
            onClick={() => onAccept([...accepted])}
          >
            {refreshing ? "accepting…" : `accept ${accepted.size}`}
          </Button>
          <Button type="button" disabled={refreshing} onClick={() => onAccept([...ids])}>
            accept all {proposals.length}
          </Button>
          <Button type="button" onClick={onClose} disabled={refreshing}>
            close
          </Button>
        </div>
      </DialogShell>
      {preview ? <ThreadPreview thread={preview} onClose={() => setPreview(null)} /> : null}
    </>
  );
}
