import { createRoot } from "react-dom/client";
import { StrictMode, useEffect, useMemo, useRef, useState } from "react";
import { QueryClientProvider } from "@tanstack/react-query";
import { Timeline } from "./components/Timeline";
import { attach } from "./client/behaviour";
import { derive } from "./lib/derive";
import { SpecView } from "./components/SpecView";
import { loadSpec } from "./lib/loadSpec";
import { makeQueryClient } from "./lib/queryClient";
import { normalise } from "./lib/normalise";
import { SelectView } from "./components/Select";
import { parseRoute, type Route } from "./lib/route";
import type { Timeline as Spec } from "./lib/spec";
import "./styles.css";
import "./select.css";

/**
 * Three ways in, by URL: the search route at /, the render route /view/<name>
 * (a page POST /v1/spec saved, reloadable after a refresh or a reboot), and a
 * static spec named by ?spec= (a file the static pipeline wrote, or the
 * fixtures). A spec dropped on the page joins as a fourth way that owns no URL
 * of its own.
 *
 * ?spec= stays because a spec on disk is how one gets inspected, and
 * scripts/render.tsx still writes those files — the API is another source, not a
 * replacement for the one the static pipeline uses.
 */
export function App() {
  const [spec, setSpec] = useState<Spec | null>(null);
  const [error, setError] = useState<string | null>(null);
  const param = useMemo(() => new URLSearchParams(location.search).get("spec"), []);
  const [route, setRoute] = useState<Route>(parseRoute);
  // The name of the spec in state when it came from the API, so the just-built
  // page is not fetched back out of the saved file the moment it is shown.
  const loaded = useRef<string | null>(null);

  // Browser back/forward move between / and /view/<name>; the server answers
  // /view/<name> with the same shell, so the route survives a reload.
  useEffect(() => {
    const onPop = () => setRoute(parseRoute());
    addEventListener("popstate", onPop);
    return () => removeEventListener("popstate", onPop);
  }, []);

  // A spec dropped on the page is a fourth way in that owns no URL of its own:
  // it joins under whatever route is current, and Back returns to the search
  // when it was dropped there.
  useEffect(() => {
    const onDrop = async (ev: DragEvent) => {
      ev.preventDefault();
      const f = ev.dataTransfer?.files?.[0];
      if (f) {
        try {
          setSpec(normalise(JSON.parse(await f.text())));
          setError(null);
        } catch (e) {
          setError(String(e));
        }
      }
    };
    const stop = (ev: DragEvent) => ev.preventDefault();
    addEventListener("drop", onDrop);
    addEventListener("dragover", stop);
    return () => {
      removeEventListener("drop", onDrop);
      removeEventListener("dragover", stop);
    };
  }, []);

  // The search route holds no page: a rendered spec under / was built for a
  // URL that no longer names it, so it is dropped rather than shown orphaned.
  useEffect(() => {
    if (route.view === "search") {
      setSpec(null);
      loaded.current = null;
    }
  }, [route]);

  // The static ?spec= way in, unchanged: the URL names a file, so it is
  // fetched as-is and the page offers no way back to a search it was never
  // the result of.
  useEffect(() => {
    if (param === null) return;
    loaded.current = null;
    let cancelled = false;
    loadSpec(param)
      .then((sp) => {
        if (!cancelled) setSpec(sp);
      })
      .catch((e) => {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e));
      });
    return () => {
      cancelled = true;
    };
  }, [param]);

  // The render route: /view/<name> loads the saved page from the API, so a
  // refresh lands back on the same page — the URL is the durable handle.
  const viewName = route.view === "render" ? route.name : null;
  useEffect(() => {
    if (viewName === null) return;
    if (loaded.current === viewName) return; // just built it; it is in hand
    loaded.current = viewName;
    let cancelled = false;
    setSpec(null);
    loadSpec(`/v1/specs/${encodeURIComponent(viewName)}`)
      .then((sp) => {
        if (!cancelled) setSpec(sp);
      })
      .catch((e) => {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e));
      });
    return () => {
      cancelled = true;
    };
  }, [viewName]);

  if (error)
    return (
      <pre style={{ padding: "2rem", whiteSpace: "pre-wrap", color: "crimson" }}>
        {error}
        {"\n\nOr drop a spec JSON onto the page."}
      </pre>
    );
  // A spec is on screen. Back is offered only where the spec owns no URL: a
  // drop on the search route. A page under /view/<name> just is, and a ?spec=
  // page is what it is.
  if (spec)
    return (
      <Rendered
        spec={spec}
        onBack={route.view === "search" && param === null ? () => setSpec(null) : undefined}
      />
    );
  if (viewName !== null) return <p style={{ padding: "2rem", opacity: 0.6 }}>Loading page…</p>;
  if (param !== null) return <p style={{ padding: "2rem", opacity: 0.6 }}>Loading spec…</p>;
  return (
    <SelectView
      onBuilt={(sp, name) => {
        // Building is the one place a page earns its URL: pushState moves the
        // address bar to /view/<name> without reloading, and the spec already
        // in hand is shown rather than fetched back.
        loaded.current = name;
        setSpec(sp);
        setError(null);
        history.pushState(null, "", `/view/${name}`);
        setRoute(parseRoute());
      }}
    />
  );
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
