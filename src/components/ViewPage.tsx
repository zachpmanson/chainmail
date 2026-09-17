import { useEffect, useRef, useState } from "react";
import { Link, useParams } from "@tanstack/react-router";
import { ApiError, $api, type RefreshReport } from "../lib/api";
import { normalise } from "../lib/normalise";
import type { Timeline } from "../lib/spec";
import { MEDIA_BASE, pullSummary } from "../lib/attachments";
import { Rendered } from "./Rendered";

/**
 * One line saying what a refresh did. NothingNew is the calm default: a page
 * that was already current should not read as if it changed. The other states
 * are the four thread lists the report can hold, joined by comma, plus the
 * searches it recorded (the add-email search) and any twins it collapsed. A
 * page that changed only its counts (entries) is still reported — those are the
 * chains a reader can see grew.
 */
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

/**
 * The page route /view/<name>: load the page POST /v1/spec saved under that
 * name, and offer refresh — the read half of the CLI's `refresh` command,
 * available only here, where the name lets the server rewrite the file too, so
 * a reload lands on the same run.
 *
 * Refresh asks the server to slurp before it re-derives: POST /v1/slurp is the
 * fetching half, and a host that was not started with -slurp answers 403, which
 * leaves exactly the old behaviour — re-derive what the corpus already holds.
 * So the button is always worth pressing; on a slurp host it also brings in mail
 * that arrived since the last cron ingest.
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
  // accepted (a count alone would hide what was found).
  const [local, setLocal] = useState<Timeline | null>(null);
  const [report, setReport] = useState<RefreshReport | null>(null);
  // The message whose files are being fetched: its button says so, and every
  // other one is held until this pull and the rebuild behind it have finished.
  const [pulling, setPulling] = useState<string | null>(null);
  // Why the last pull failed, when it did. Console-only went with the refresh
  // verdict and was wrong for this one: a pull is slow (mailbox round trips,
  // then a page rebuild), it costs quota, and "nothing happened" is
  // indistinguishable from "it failed" without it.
  const [pullNote, setPullNote] = useState<string | null>(null);

  // A different page means a different run: drop the refreshed copy and any
  // report from the previous one.
  useEffect(() => {
    setLocal(null);
    setReport(null);
  }, [name]);

  const refresh = $api.useMutation("post", "/v1/refresh", {
    onSuccess: (data) => {
      setLocal(normalise(data.spec));
      setReport(data.report);
      // The verdict used to float in a corner box that outlived the click; it
      // is console-only now — the page's job is the page, the log's is the log.
      console.log(refreshSummary(data.report));
    },
    onError: (e) => {
      setReport(null);
      console.error("refresh failed:", e instanceof Error ? e.message : String(e));
    },
  });

  // Slurp, then refresh. The fetch is the server's job — POST /v1/slurp, which
  // it runs only when it was started with -slurp. A 403 is not a failure here,
  // it is the read-most fallback, so the refresh runs either way: a rebuild is
  // always safe and always the point of the button.
  //
  // OnSettled needs the spec the click saw, but this hook is defined before the
  // null-check narrows `spec`, so the click snapshots it into a ref first.
  const specRef = useRef<Timeline | null>(null);
  const slurp = $api.useMutation("post", "/v1/slurp", {
    onSuccess: (data) =>
      console.log(data.report?.trim() || "slurp: nothing to report"),
    onError: (e) =>
      console.error(
        e instanceof ApiError && e.status === 403
          ? "no mailbox reach on this host (the server was started without -slurp), so this re-derives what the corpus already holds"
          : `slurp failed: ${e instanceof Error ? e.message : String(e)}`,
      ),
    onSettled: () => {
      const s = specRef.current;
      // The wire spec is exactly the shape the server accepts: optional
      // specVersion/theme/kind, no renderer-only fields. The renderer's
      // Timeline is a superset (openItemsTitle and friends), which assignment
      // allows, so the loaded spec goes straight out.
      if (s) refresh.mutate({ body: { spec: s, name, includeNew: false } });
    },
  });

  // Fetch one message's attachment bytes: the pull writes into the corpus and
  // the page is a picture of the corpus, so the pictures appear by rebuilding
  // the page rather than by patching a chip.
  //
  // The rebuild is the server's, and the page's name is what it needs: it
  // re-derives the saved page and rewrites it while the fetch is still in
  // flight for us, so the bytes land on the page even if this browser has
  // already navigated away — which is what a reload during a fetch used to
  // cost, since a browser cancels its own request when the reader leaves and
  // the refresh that made the files visible was the client's to run. The
  // refreshed page comes back on this response, so one call is the whole
  // operation and there is no second round trip to be abandoned.
  //
  // `pulling` covers both halves — the fetch and the rebuild behind it — so the
  // button cannot be pressed twice in a row against a page already being
  // replaced.
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
      // No page came back: the server was given no name, or its rebuild could
      // not run. The bytes are stored either way, so the page is re-derived
      // here instead — the old behaviour, and still the right fallback.
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
      // Said on the page, not only in the console: a press that spends mailbox
      // round trips and then fails must not look like nothing happening.
      setPullNote(
        e instanceof ApiError && e.status === 403
          ? "This host cannot fetch files (it was started without -media)."
          : `Fetching the files failed: ${e instanceof Error ? e.message : String(e)}. Nothing was stored — press again to retry.`,
      );
    },
  });

  // Accept, by root ext id, a thread the queries proposed but did not include.
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
    <>
      {pullNote && (
        <p className="pullnote" role="status">
          {pullNote}
        </p>
      )}
      <Rendered
      spec={spec}
      onRefresh={() => {
        specRef.current = spec;
        slurp.mutate({});
      }}
      onAccept={(ids) =>
        refresh.mutate({ body: { spec, name, accept: ids } })
      }
      // Adding a thread by hand sends the search that found it as well, so the
      // page records it: an accepted thread whose query the spec does not hold
      // is one nothing can explain or re-find. The server dedupes it against
      // what the spec already records, so re-adding from the same search is
      // harmless. The modal searches hybrid, and says so in the note.
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
      // The endpoint stored bytes come from. Named here and nowhere else,
      // because this component is the app: the static export renders Timeline
      // without it, and a shared page has no server to point at.
      mediaBase={MEDIA_BASE}
      report={report}
      refreshing={slurp.isPending || refresh.isPending}
    />
    </>
  );
}