import { useState } from "react";
import type { OrgRule } from "../../lib/api/api";
import StatusBadge from "../ui/StatusBadge";
import Button from "../ui/Button";
import TextInput from "../ui/TextInput";

/** `org: null` drops the rule; `""` rules the domain is no organisation. */
export type Draft = { domain: string; org: string | null };

export default function RuleRow({
  d,
  busy,
  onPick,
}: {
  d: OrgRule;
  busy: boolean;
  onPick: (draft: Draft) => void;
}) {
  const [draft, setDraft] = useState<string | null>(null);
  const value = draft ?? d.org ?? "";
  const edited = draft !== null && draft !== (d.org ?? "");
  const mail = `${d.messages} message${d.messages === 1 ? "" : "s"} from ${d.people} sender${
    d.people === 1 ? "" : "s"
  }`;
  return (
    <article>
      <p className="mt-0 mb-1.5 text-2xs font-bold uppercase tracking-[.05em] text-muted">
        <code>{d.domain}</code>
        {d.stored ? (
          <StatusBadge tone="success" className="ml-2">
            yours
          </StatusBadge>
        ) : (
          <StatusBadge tone="neutral">guessed</StatusBadge>
        )}
      </p>
      <p className="my-0.5 text-sm/snug [&_code]:text-xs [&_code]:wrap-anywhere">
        {mail} — drawn as{" "}
        {d.org ? <code>{d.org}</code> : <span className="text-muted">no organisation</span>}
        {d.stored && d.guess ? (
          <span className="text-muted"> — cleared, it is read as {d.guess}</span>
        ) : null}
      </p>
      <p className="my-0.5 text-sm/snug [&_code]:text-xs [&_code]:wrap-anywhere">
        <TextInput
          className="min-w-0 flex-[0_1_12rem] mr-2 px-1.5 py-1 text-xs"
          value={value}
          disabled={busy}
          placeholder="no rule"
          aria-label={`Organisation for ${d.domain}`}
          onChange={(e) => setDraft(e.target.value)}
        />
        <Button
          type="button"
          variant="subtle"
          density="compact"
          disabled={busy || !edited}
          onClick={() => {
            setDraft(null);
            onPick({ domain: d.domain, org: value.trim() });
          }}
        >
          {value.trim() === "" ? "save — nobody's" : "save"}
        </Button>
        {d.stored ? (
          <Button
            type="button"
            variant="subtle"
            density="compact"
            disabled={busy}
            onClick={() => {
              setDraft(null);
              onPick({ domain: d.domain, org: null });
            }}
          >
            clear
          </Button>
        ) : null}
      </p>
    </article>
  );
}
