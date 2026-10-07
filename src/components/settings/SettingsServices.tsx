import type { ServiceStatus, StatusResponse } from "../../lib/api/api";
import { when } from "../../lib/ui/stamp";
import SettingsSection from "./SettingsSection";
import InlineAlert from "../ui/InlineAlert";
import { errText } from "../../lib/ui/errText";

/** A service's reported state and detail, as one settings-list row. */
function ServiceRow({ svc }: { svc: ServiceStatus }) {
  const badges: Record<ServiceStatus["status"], { word: string; className: string }> = {
    ok: {
      word: "logged in",
      className:
        "inline-flex whitespace-nowrap rounded-full border border-current px-2 py-0.5 text-2xs font-bold text-green-800",
    },
    "needs-auth": {
      word: "needs auth",
      className:
        "inline-flex whitespace-nowrap rounded-full border border-current px-2 py-0.5 text-2xs font-bold text-amber-700",
    },
    down: {
      word: "down",
      className:
        "inline-flex whitespace-nowrap rounded-full border border-current px-2 py-0.5 text-2xs font-bold text-red-700",
    },
    unchecked: {
      word: "unchecked",
      className:
        "inline-flex whitespace-nowrap rounded-full border border-current px-2 py-0.5 text-2xs font-bold text-muted",
    },
  };
  const badge = badges[svc.status] ?? badges.unchecked;
  return (
    <li className="flex flex-wrap items-center gap-x-3 gap-y-2 py-2">
      <span className={badge.className}>{badge.word}</span>
      <span className="min-w-32 text-sm font-semibold">{svc.label}</span>
      {svc.detail ? (
        <span className="flex-[1_1_12rem] text-xs wrap-break-word text-muted">{svc.detail}</span>
      ) : null}
    </li>
  );
}

/** Service health and corpus snapshot are read-only sections with independent data. */
export default function SettingsServices({
  status,
}: {
  status: {
    data?: StatusResponse;
    isError: boolean;
    error: unknown;
    isPending: boolean;
  };
}) {
  return (
    <SettingsSection
      id="services-heading"
      title="Connected services"
      description={`Run corpus status to refresh.${status.data?.checkedAt ? ` Last checked ${when(status.data.checkedAt)}.` : " Nothing measured yet."}`}
    >
      {status.isError ? <InlineAlert compact>{errText(status.error)}</InlineAlert> : null}
      <ul className="mt-3 list-none border-t border-line p-0">
        {status.isPending ? (
          <li className="flex flex-wrap items-center gap-x-3 gap-y-2 py-2 text-xs text-muted">
            Checking services…
          </li>
        ) : status.data?.services.length ? (
          status.data.services.map((svc) => <ServiceRow key={svc.id} svc={svc} />)
        ) : (
          <li className="flex flex-wrap items-center gap-x-3 gap-y-2 py-2 text-xs text-muted">
            No services reported.
          </li>
        )}
      </ul>
    </SettingsSection>
  );
}
