import { useEffect, useState } from "react";
import { ApiError, $api, type RefreshReport } from "../api/api";
import { errText } from "../ui/errText";
import { pushToast, SAID_MS } from "../ui/toasts";
import { normalise } from "./normalise";
import type { Timeline } from "./spec";

function refreshSummary(r: RefreshReport): string {
  if (r.nothingNew) return "Page already up to date";
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
  return parts.length ? `Page refreshed: ${parts.join(", ")}` : "Page refreshed; nothing changed";
}

/** The page shown: the saved one until a refresh, slurp or pull hands back a rebuilt one. */
export function useSpecRefresh(name: string, saved: Timeline | null) {
  const [local, setLocal] = useState<Timeline | null>(null);
  const [report, setReport] = useState<RefreshReport | null>(null);

  useEffect(() => {
    setLocal(null);
    setReport(null);
  }, [name]);

  const refresh = $api.useMutation("post", "/v1/refresh", {
    onSuccess: (data) => {
      setLocal(normalise(data.spec));
      setReport(data.report);
      pushToast(refreshSummary(data.report), "note", SAID_MS);
    },
    onError: (e) => {
      setReport(null);
      pushToast(`Refresh failed: ${errText(e)}`, "fail");
    },
  });

  // A 403 just means the host lacks -slurp; the refresh after it runs regardless.
  const slurp = $api.useMutation("post", "/v1/slurp", {
    onSuccess: (data) =>
      pushToast(
        `Fetched mail${data.report?.trim() ? `: ${data.report.trim().split("\n").at(-1)}` : ""}`,
        "note",
        SAID_MS,
      ),
    onError: (e) =>
      e instanceof ApiError && e.status === 403
        ? pushToast(
            "This host can't reach the mailbox (no -slurp); re-deriving the page.",
            "note",
            SAID_MS,
          )
        : pushToast(`Fetching mail failed: ${errText(e)}`, "fail"),
  });

  const spec = local ?? saved;

  return {
    spec,
    report,
    refresh,
    refreshing: slurp.isPending || refresh.isPending,
    /** Slurps the mailbox, then re-derives the page whether or not that worked. */
    reslurp: () => {
      if (!spec) return;
      void slurp
        .mutateAsync({})
        .catch(() => {})
        .then(() => refresh.mutate({ body: { spec, name, includeNew: false } }));
    },
    /** Shows a page the server already rebuilt. */
    adopt: (rebuilt: unknown, rebuiltReport: RefreshReport | undefined) => {
      setLocal(normalise(rebuilt));
      setReport(rebuiltReport ?? null);
    },
  };
}
