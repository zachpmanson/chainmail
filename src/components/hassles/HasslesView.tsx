import useNativeOperations from "../../lib/hassles/useNativeOperations";
import HassleRecovery from "./HassleRecovery";
import { useEffect } from "react";
import { $api } from "../../lib/api/api";
import { locationKey } from "../../lib/hassles/forms";
import { errText } from "../../lib/ui/errText";
import ButtonLink from "../ui/ButtonLink";
import InlineAlert from "../ui/InlineAlert";
import HassleInventory from "./HassleInventory";

export default function HasslesView() {
  useEffect(() => {
    const previous = document.title;
    document.title = "Hassles";
    return () => {
      document.title = previous;
    };
  }, []);
  const config = $api.useQuery("get", "/v1/hassles/location", {}, { staleTime: 0 });
  const native = useNativeOperations();
  const location = config.data?.location;
  const selected = !!location?.accountId && !!location.calendarId;
  return (
    <main
      aria-label="Hassles"
      className="@container mx-auto box-border flex w-full max-w-[125rem] flex-col gap-4 px-4 py-5"
    >
      <HassleRecovery />
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
          disabled={config.isError || native.busy}
        />
      ) : null}
    </main>
  );
}
