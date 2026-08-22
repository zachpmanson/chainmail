import { createRoot } from "react-dom/client";
import { StrictMode, useEffect, useMemo, useState } from "react";
import { QueryClientProvider } from "@tanstack/react-query";
import { Timeline } from "./components/Timeline";
import { attach } from "./client/behaviour";
import { derive } from "./lib/derive";
import { SpecView } from "./components/SpecView";
import { loadSpec } from "./lib/loadSpec";
import { makeQueryClient } from "./lib/queryClient";
import { normalise } from "./lib/normalise";
import { SelectView } from "./components/Select";
import type { Timeline as Spec } from "./lib/spec";
import "./styles.css";
import "./select.css";

/**
 * Three ways in, in priority order: a spec named by ?spec=, a spec dropped on
 * the page, or the search-and-select stage against the API.
 *
 * ?spec= stays because a spec on disk is how one gets inspected, and
 * scripts/render.tsx still writes those files — the API is another source, not a
 * replacement for the one the static pipeline uses.
 */
export function App() {
  const [spec, setSpec] = useState<Spec | null>(null);
  const [error, setError] = useState<string | null>(null);
  const param = useMemo(() => new URLSearchParams(location.search).get("spec"), []);

  useEffect(() => {
    if (param === null) return;
    loadSpec(param)
      .then(setSpec)
      .catch((e) => setError(e instanceof Error ? e.message : String(e)));
  }, [param]);

  useEffect(() => {
    const onDrop = async (ev: DragEvent) => {
      ev.preventDefault();
      const f = ev.dataTransfer?.files?.[0];
      if (f) {
        try { setSpec(normalise(JSON.parse(await f.text()))); setError(null); }
        catch (e) { setError(String(e)); }
      }
    };
    const stop = (ev: DragEvent) => ev.preventDefault();
    addEventListener("drop", onDrop);
    addEventListener("dragover", stop);
    return () => { removeEventListener("drop", onDrop); removeEventListener("dragover", stop); };
  }, []);

  if (error)
    return (
      <pre style={{ padding: "2rem", whiteSpace: "pre-wrap", color: "crimson" }}>
        {error}
        {"\n\nOr drop a spec JSON onto the page."}
      </pre>
    );
  if (spec)
    // A spec named on the URL is what the page is for, so it offers no way back
    // to a search it was never the result of.
    return <Rendered spec={spec} onBack={param === null ? () => setSpec(null) : undefined} />;
  if (param !== null) return <p style={{ padding: "2rem", opacity: 0.6 }}>Loading spec…</p>;
  return <SelectView onBuilt={setSpec} />;
}

/**
 * Holds the chain filter and re-attaches behaviour whenever the transcript is
 * rebuilt. Excluding a chain re-derives ordering, lanes, spines, the minimap and
 * every count — hiding rows would leave holes in the grid and mis-drawn lanes.
 */
function Rendered({ spec, onBack }: { spec: Spec; onBack?: () => void }) {
  const [excluded, setExcluded] = useState<Set<string>>(new Set());
  const [showSpec, setShowSpec] = useState(false);

  // chains of the UNFILTERED trail, so an excluded one stays listed and checkable
  const all = useMemo(() => derive(spec), [spec]);
  const chains = useMemo(
    () =>
      all.layout.chains.map((c) => {
        // link to whichever entry in the chain names a mailbox id; a fully
        // unspooled chain may have none, in which case only the anchor is offered
        const withId = c.entries
          .map((id) => all.rows.find((r) => r.id === id)!)
          .find((r) => r.entry.threadId ?? r.entry.gmailId);
        return {
          root: c.root,
          subject: c.subject,
          opener: c.opener,
          date: c.date,
          count: c.entries.length,
          gmailId: withId?.entry.threadId ?? withId?.entry.gmailId,
          anchor: c.root,
        };
      }),
    [all],
  );

  const filtered = useMemo<Spec>(() => {
    if (excluded.size === 0) return spec;
    const keep = all.rows
      .filter((r) => !excluded.has(r.chain))
      .map((r) => r.entry);
    return { ...spec, messages: keep as Spec["messages"] };
  }, [spec, all, excluded]);

  const onToggle = (root: string) =>
    setExcluded((prev) => {
      const next = new Set(prev);
      if (next.has(root)) next.delete(root);
      else next.add(root);
      return next;
    });

  const empty = filtered.messages.length === 0;

  useEffect(() => {
    if (empty) return;
    // StrictMode double-invokes effects in dev; attach() returns a cleanup so the
    // second pass does not stack duplicate listeners
    const detach = attach(document);
    document.body.classList.add("hasmap");
    return detach;
  }, [filtered, empty]);

  if (empty) {
    return (
      <div className="wrap">
        <p style={{ padding: "2rem", color: "var(--muted)" }}>
          Every chain is excluded. Re-enable one from Sources &amp; provenance —
          reload to reset.
        </p>
      </div>
    );
  }

  return (
    <>
      {onBack ? (
        <button type="button" className="selback" onClick={onBack}>
          ← choose chains
        </button>
      ) : null}
      <Timeline
        spec={filtered}
        filter={{ chains, excluded, onToggle }}
        onShowSpec={() => setShowSpec(true)}
      />
      {showSpec ? <SpecView spec={filtered} onClose={() => setShowSpec(false)} /> : null}
    </>
  );
}

const root = document.getElementById("root");
if (root)
  createRoot(root).render(
    <StrictMode>
      <QueryClientProvider client={makeQueryClient()}>
        <App />
      </QueryClientProvider>
    </StrictMode>,
  );
