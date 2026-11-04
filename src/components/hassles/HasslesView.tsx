import { useIsMutating } from "@tanstack/react-query";
import { $api } from "../../lib/api/api";
import { locationKey } from "../../lib/hassles/forms";
import { errText } from "../../lib/ui/errText";
import ButtonLink from "../ui/ButtonLink";
import InlineAlert from "../ui/InlineAlert";
import HassleInventory from "./HassleInventory";

export default function HasslesView() {
  const config = $api.useQuery("get", "/v1/hassles/location", {}, { staleTime: 0 });
  const mutations = useIsMutating({
    predicate: (m) =>
      m.options.mutationKey?.some((k) => typeof k === "string" && k.startsWith("/v1/hassles")) ??
      false,
  });
  const location = config.data?.location;
  const selected = !!location?.accountId && !!location.calendarId;
  return (
    <main className="mx-auto flex max-w-4xl flex-col gap-4 px-4 py-5">
      <h1 className="m-0 text-2xl">Hassles</h1>
      {config.isError ? <InlineAlert>{errText(config.error)}</InlineAlert> : null}
      {config.isPending ? <p>Reading reminder settings…</p> : null}
      {config.data && !config.data.enabled ? (
        <p>Hassles is disabled. Calendar access must be enabled by the operator.</p>
      ) : null}
      {config.data && !selected ? (
        <ButtonLink href="/settings#hassles-heading">
          Choose a dedicated reminder calendar in Settings
        </ButtonLink>
      ) : null}
      {config.data?.enabled && selected && location ? (
        <HassleInventory
          key={locationKey(location)}
          location={location}
          disabled={mutations > 0 || config.isFetching || config.isError}
        />
      ) : null}
    </main>
  );
}
