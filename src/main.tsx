import { createRoot } from "react-dom/client";
import { StrictMode, useEffect, useState } from "react";
import { Timeline } from "./components/Timeline";
import { loadSpec } from "./lib/loadSpec";
import { normalise } from "./lib/normalise";
import type { Timeline as Spec } from "./lib/spec";
import "./styles.css";

/** Dev shell: load a spec by ?spec=, or let one be dropped on the page. */
function App() {
  const [spec, setSpec] = useState<Spec | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const param = new URLSearchParams(location.search).get("spec") ?? "synthetic.json";
    loadSpec(param)
      .then(setSpec)
      .catch((e) => setError(e instanceof Error ? e.message : String(e)));
  }, []);

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
  if (!spec) return <p style={{ padding: "2rem", opacity: 0.6 }}>Loading spec…</p>;
  return <Timeline spec={spec} />;
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
