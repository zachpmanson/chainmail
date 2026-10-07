import { useEffect, useState } from "react";
import DialogShell from "../ui/DialogShell";
import FormField from "../ui/FormField";
import Button from "../ui/Button";
import TextInput from "../ui/TextInput";
import ThreadPreview from "../thread/ThreadPreview";
import FullThreadRow from "../inbox/FullThreadRow";
import RankMeta from "../inbox/RankMeta";
import { $api, searchQuery, type ChainHit } from "../../lib/api/api";
import { errText } from "../../lib/ui/errText";

/** Sends the query with the chosen roots so the page records it and refreshes can
 *  re-find the thread. */
export default function AddEmailsModal({
  onClose,
  onAdd,
}: {
  onClose: () => void;
  onAdd: (ids: string[], query: string) => void;
}) {
  const [q, setQ] = useState("");
  const [asked, setAsked] = useState<string | null>(null);
  const [chosen, setChosen] = useState<string[]>([]);
  const [preview, setPreview] = useState<ChainHit | null>(null);

  const results = $api.useQuery(
    "get",
    "/v1/search",
    { params: { query: asked ? searchQuery({ q: asked, mode: "hybrid" }) : {} } },
    { enabled: asked !== null },
  );

  useEffect(() => {
    const onKey = (ev: KeyboardEvent) => {
      if (ev.key === "Escape") onClose();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [onClose]);

  const submit = (ev: React.FormEvent) => {
    ev.preventDefault();
    const t = q.trim();
    if (!t) return;
    setChosen([]);
    setAsked(t);
  };

  const toggle = (root: string) =>
    setChosen((prev) => (prev.includes(root) ? prev.filter((r) => r !== root) : [...prev, root]));

  const chains = results.data?.chains ?? [];
  return (
    <>
      <DialogShell label="Add another email" onBackdropClick={onClose}>
        <div className="flex items-center gap-2 border-b border-line px-3 py-2">
          <b className="text-xs font-bold tracking-[.09em] text-muted uppercase">add email</b>
          <span className="ml-auto text-xs text-muted">
            search the corpus for a thread to add to this page
          </span>
        </div>
        <form className="flex items-center gap-2 border-b border-line px-3 py-2" onSubmit={submit}>
          <FormField
            className="flex min-w-0 flex-1 items-center gap-1.5 text-xs text-muted"
            label="Query"
          >
            <TextInput
              autoFocus
              value={q}
              onChange={(e) => setQ(e.target.value)}
              className="min-w-0 flex-1 rounded-md border border-line bg-bg px-2 py-1 text-xs text-fg"
              placeholder="words, a name, an id"
              aria-label="Search query"
            />
          </FormField>
          <Button
            density="compact"
            className="cursor-pointer bg-bg disabled:cursor-default disabled:opacity-45"
            type="submit"
            disabled={!q.trim()}
          >
            Search
          </Button>
        </form>
        {results.isError ? (
          <p className="mt-2 flex-[1_1_100%] text-xs text-muted" role="alert">
            {errText(results.error)}
          </p>
        ) : null}
        {results.isFetching ? (
          <p className="mt-2 flex-[1_1_100%] text-xs text-muted">Searching…</p>
        ) : null}
        {asked && !results.isFetching && !results.isError && chains.length === 0 ? (
          <p className="mt-2 flex-[1_1_100%] text-xs text-muted">No thread matched.</p>
        ) : null}
        {chains.length > 0 ? (
          <ul className="m-0 flex list-none flex-col gap-1.5 overflow-auto px-3 py-2">
            {chains.map((c) => (
              <FullThreadRow
                key={c.rootExtId}
                thread={c}
                checked={chosen.includes(c.rootExtId)}
                current={false}
                meta={<RankMeta thread={c} />}
                onToggle={() => toggle(c.rootExtId)}
                onOpen={() => setPreview(c)}
              />
            ))}
          </ul>
        ) : null}
        <div className="flex justify-end gap-2 border-t border-line px-3 py-2">
          <Button
            type="button"
            disabled={chosen.length === 0}
            onClick={() => {
              // The search that ran, not whatever the box holds now.
              if (asked) onAdd([...chosen], asked);
            }}
          >
            {`add ${chosen.length} to page`}
          </Button>
          <Button type="button" onClick={onClose}>
            close
          </Button>
        </div>
      </DialogShell>
      {preview ? <ThreadPreview thread={preview} onClose={() => setPreview(null)} /> : null}
    </>
  );
}
