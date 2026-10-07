import { useEffect, useRef, useState } from "react";
import { Link, useParams } from "@tanstack/react-router";
import { ApiError, $api, type RefreshReport } from "../../lib/api/api";
import { normalise } from "../../lib/timeline/normalise";
import type { Timeline } from "../../lib/timeline/spec";
import { MEDIA_BASE, pullSummary } from "../../lib/message/attachments";
import Rendered from "./Rendered";

function refreshSummary(r: RefreshReport): string {
  if (r.nothingNew) return "already up to date";
  const parts: string[] = [];
  if (r.chainsAdded?.length) parts.push(`${r.chainsAdded.length} added`);
  if (r.chainsGrown?.length) parts.push(`${r.chainsGrown.length} grew`);
  if (r.chainsProposed?.length) parts.push(`${r.chainsProposed.length} proposed`);
  if (r.chainsUnranked?.length) parts.push(`${r.chainsUnranked.length} unranked`);
  if (r.queriesRecorded?.length)
    parts.push(
      `${r.queriesRecorded.length} search${r.queriesRecorded.length === 1 ? "" : "es"} recorded`,
    );
  if (r.twinsCollapsed)
    parts.push(`${r.twinsCollapsed} twin ${r.twinsCollapsed === 1 ? "copy" : "copies"} collapsed`);
  return parts.length ? `refresh: ${parts.join(", ")}` : "refresh: nothing changed";
}

/** POST /v1/slurp answers 403 on hosts without -slurp; refresh re-derives regardless. */
export default function ViewPage() {
  const { name } = useParams({ from: "/view/$name" });
  const fetched = $api.useQuery("get", "/v1/specs/{name}", {
    params: { path: { name } },
  });
  const [local, setLocal] = useState<Timeline | null>(null);
  const [report, setReport] = useState<RefreshReport | null>(null);
  // Holds every other button until this pull and the rebuild behind it finish.
  const [pulling, setPulling] = useState<string | null>(null);
  const [pullNote, setPullNote] = useState<string | null>(null);

  useEffect(() => {
    setLocal(null);
    setReport(null);
  }, [name]);

  const refresh = $api.useMutation("post", "/v1/refresh", {
    onSuccess: (data) => {
      setLocal(normalise(data.spec));
      setReport(data.report);
      console.log(refreshSummary(data.report));
    },
    onError: (e) => {
      setReport(null);
      console.error("refresh failed:", e instanceof Error ? e.message : String(e));
    },
  });

  // A slurp 403 just means the host lacks -slurp; refresh anyway.
  // specRef: onSettled needs the spec, but this hook runs before the null-check narrows it.
  const specRef = useRef<Timeline | null>(null);
  const slurp = $api.useMutation("post", "/v1/slurp", {
    onSuccess: (data) => console.log(data.report?.trim() || "slurp: nothing to report"),
    onError: (e) =>
      console.error(
        e instanceof ApiError && e.status === 403
          ? "no mailbox reach on this host (the server was started without -slurp), so this re-derives what the corpus already holds"
          : `slurp failed: ${e instanceof Error ? e.message : String(e)}`,
      ),
    onSettled: () => {
      const s = specRef.current;
      if (s) refresh.mutate({ body: { spec: s, name, includeNew: false } });
    },
  });

  // Sends the page name so the server rebuilds and saves the page itself; the bytes
  // then land even if this browser navigates away mid-request.
  const pull = $api.useMutation("post", "/v1/media/pull", {
    onSuccess: (data) => {
      console.log(`fetch: ${pullSummary(data)}`);
      setPullNote(null);
      if (data.spec) {
        setLocal(normalise(data.spec));
        setReport(data.report ?? null);
        setPulling(null);
        return;
      }
      // No page came back (no name, or the rebuild failed): re-derive client-side.
      const s = specRef.current;
      if (!s) {
        setPulling(null);
        return;
      }
      refresh.mutate(
        { body: { spec: s, name, includeNew: false } },
        { onSettled: () => setPulling(null) },
      );
    },
    onError: (e) => {
      setPulling(null);
      setPullNote(
        e instanceof ApiError && e.status === 403
          ? "This host cannot fetch files (it was started without -media)."
          : `Fetching the files failed: ${e instanceof Error ? e.message : String(e)}. Nothing was stored — press again to retry.`,
      );
    },
  });

  const spec = local ?? (fetched.data ? normalise(fetched.data) : null);

  if (fetched.isError)
    return (
      <div className="mx-auto max-w-[76rem] px-5 pt-7 pb-14">
        <p className="p-8 text-muted">
          No saved page named <strong>{name}</strong> — build one from a <Link to="/">search</Link>.
        </p>
      </div>
    );
  if (!spec) return <p style={{ padding: "2rem", opacity: 0.6 }}>Loading page…</p>;

  return (
    <>
      {pullNote && (
        <p
          className="mb-3 mt-0 border border-accent border-l-[3px] rounded-md bg-card px-3 py-2 text-[.85rem] text-fg"
          role="status"
        >
          {pullNote}
        </p>
      )}
      <Rendered
        spec={spec}
        onRefresh={() => {
          specRef.current = spec;
          slurp.mutate({});
        }}
        onAccept={(ids) => refresh.mutate({ body: { spec, name, accept: ids } })}
        // Send the query too, so the page records how the thread was found; the server dedupes it.
        onAdd={(ids, query) =>
          refresh.mutate({
            body: {
              spec,
              name,
              accept: ids,
              queries: [{ q: query, note: "add-email search, mode=hybrid" }],
            },
          })
        }
        onPull={(extId) => {
          specRef.current = spec;
          setPulling(extId);
          setPullNote(null);
          pull.mutate({ body: { entry: extId, name } });
        }}
        pulling={pulling}
        // Only the app has a server; the static export renders Timeline without mediaBase.
        mediaBase={MEDIA_BASE}
        report={report}
        refreshing={slurp.isPending || refresh.isPending}
      />
    </>
  );
}
