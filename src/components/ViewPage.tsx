import { useEffect, useState } from "react";
import { Link, useParams } from "@tanstack/react-router";
import { $api, type RefreshReport } from "../lib/api";
import { normalise } from "../lib/normalise";
import type { Timeline } from "../lib/spec";
import { Rendered } from "./Rendered";

/**
 * One line saying what a refresh did. NothingNew is the calm default: a page
 * that was already current should not read as if it changed. The other four
 * states are the four lists the report can hold, joined by comma, and a page
 * that changed only its counts (entries) is still reported — those are the
 * chains a reader can see grew.
 */
function refreshSummary(r: RefreshReport): string {
  if (r.nothingNew) return "already up to date";
  const parts: string[] = [];
  if (r.chainsAdded?.length) parts.push(`${r.chainsAdded.length} added`);
  if (r.chainsGrown?.length) parts.push(`${r.chainsGrown.length} grew`);
  if (r.chainsProposed?.length) parts.push(`${r.chainsProposed.length} proposed`);
  if (r.chainsUnranked?.length) parts.push(`${r.chainsUnranked.length} unranked`);
  if (r.twinsCollapsed)
    parts.push(`${r.twinsCollapsed} twin ${r.twinsCollapsed === 1 ? "copy" : "copies"} collapsed`);
  return parts.length ? `refresh: ${parts.join(", ")}` : "refresh: nothing changed";
}

/**
 * The page route /view/<name>: load the page POST /v1/spec saved under that
 * name, and offer refresh — the read half of the CLI's `refresh` command,
 * available only here, where the name lets the server rewrite the file too, so
 * a reload lands on the same run. The mailbox is never reached: the corpus is
 * the cron's job, this re-derives the page from it.
 *
 * A name that was never saved is a client-side dead end with a way home; the
 * server's 404 names the missing page, and there is no point pretending a URL
 * that never resolved is anything else.
 */
export function ViewPage() {
  const { name } = useParams({ from: "/view/$name" });
  const fetched = $api.useQuery("get", "/v1/specs/{name}", {
    params: { path: { name } },
  });
  // A local, refreshed spec that wins over the stale cached fetch, plus the
  // report that produced it. The report is kept so proposals can be shown and
  // accepted (a count alone would hide what was found; see refreshNote).
  const [local, setLocal] = useState<Timeline | null>(null);
  const [refreshNote, setRefreshNote] = useState<string | null>(null);
  const [report, setReport] = useState<RefreshReport | null>(null);

  // A different page means a different run: drop the refreshed copy and any
  // note and report from the previous one.
  useEffect(() => {
    setLocal(null);
    setRefreshNote(null);
    setReport(null);
  }, [name]);

  const refresh = $api.useMutation("post", "/v1/refresh", {
    onSuccess: (data) => {
      setLocal(normalise(data.spec));
      setReport(data.report);
      setRefreshNote(refreshSummary(data.report));
    },
    onError: (e) => {
      setReport(null);
      setRefreshNote(e instanceof Error ? e.message : String(e));
    },
  });

  // Accept, by root ext id, a chain the queries proposed but did not include.
  // Re-running the refresh with accept is how a proposal becomes membership.
  // Defined where `spec` is known non-null (the guard below narrows it).

  const spec = local ?? (fetched.data ? normalise(fetched.data) : null);

  if (fetched.isError)
    return (
      <div className="wrap">
        <p style={{ padding: "2rem", color: "var(--muted)" }}>
          No saved page named <strong>{name}</strong> — build one from a{" "}
          <Link to="/">search</Link>.
        </p>
      </div>
    );
  if (!spec) return <p style={{ padding: "2rem", opacity: 0.6 }}>Loading page…</p>;

  return (
    <Rendered
      spec={spec}
      onRefresh={() =>
        refresh.mutate({
          body: {
            // The wire spec is exactly the shape the server accepts: optional
            // specVersion/theme/kind, no renderer-only fields. The renderer's
            // Timeline is a superset (openItemsTitle and friends), which
            // assignment allows, so the loaded spec goes straight out.
            spec,
            name,
            includeNew: false,
          },
        })
      }
      onAccept={(ids) =>
        refresh.mutate({ body: { spec, name, accept: ids } })
      }
      report={report}
      refreshing={refresh.isPending}
      refreshNote={refreshNote}
    />
  );
}