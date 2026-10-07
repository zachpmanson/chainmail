import { Link, useParams } from "@tanstack/react-router";
import { $api } from "../../lib/api/api";
import { MEDIA_BASE } from "../../lib/message/attachments";
import { useMediaPull } from "../../lib/thread/useMediaPull";
import { normalise } from "../../lib/timeline/normalise";
import { useSpecRefresh } from "../../lib/timeline/useSpecRefresh";
import Rendered from "./Rendered";

/** POST /v1/slurp answers 403 on hosts without -slurp; refresh re-derives regardless. */
export default function ViewPage() {
  const { name } = useParams({ from: "/view/$name" });
  const fetched = $api.useQuery("get", "/v1/specs/{name}", {
    params: { path: { name } },
  });
  const { spec, report, refresh, refreshing, reslurp, adopt } = useSpecRefresh(
    name,
    fetched.data ? normalise(fetched.data) : null,
  );

  // Sends the page name so the server rebuilds and saves the page itself; the bytes
  // then land even if this browser navigates away mid-request.
  const { pulling, pullNote, pull } = useMediaPull({
    name,
    onPulled: (data) => {
      if (data.spec) return adopt(data.spec, data.report);
      // No page came back (no name, or the rebuild failed): re-derive client-side.
      if (spec)
        return refresh.mutateAsync({ body: { spec, name, includeNew: false } }).catch(() => {});
    },
  });

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
          className="mb-3 mt-0 border border-accent border-l-3 rounded-md bg-card px-3 py-2 text-sm text-fg"
          role="status"
        >
          {pullNote}
        </p>
      )}
      <Rendered
        spec={spec}
        onRefresh={reslurp}
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
        onPull={pull}
        pulling={pulling}
        // Only the app has a server; the static export renders Timeline without mediaBase.
        mediaBase={MEDIA_BASE}
        report={report}
        refreshing={refreshing}
      />
    </>
  );
}
