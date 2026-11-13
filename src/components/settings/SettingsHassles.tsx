import useNativeOperations from "../../lib/hassles/useNativeOperations";
import HassleRecovery from "../hassles/HassleRecovery";
import { $api } from "../../lib/api/api";
import { locationKey } from "../../lib/hassles/forms";
import { errText } from "../../lib/ui/errText";
import InlineAlert from "../ui/InlineAlert";
import HassleLocationPicker from "./HassleLocationPicker";
import SettingsSection from "./SettingsSection";

export default function SettingsHassles() {
  const config = $api.useQuery("get", "/v1/hassles/location", {}, { staleTime: 0 });
  const native = useNativeOperations();
  const location = config.data?.location;
  return (
    <SettingsSection
      id="hassles-heading"
      title="Hassles"
      description="Choose one connected account and a dedicated reminder calendar."
    >
      <HassleRecovery />
      {config.isError ? <InlineAlert compact>{errText(config.error)}</InlineAlert> : null}
      {config.isPending ? <p>Reading reminder settings…</p> : null}
      {config.data && !config.data.enabled ? (
        <p className="text-xs text-muted">
          Calendar access is disabled. The operator must enable <code>-hassles</code> (Nix:{" "}
          <code>enableHassles</code>) before selecting a calendar.
        </p>
      ) : null}
      {config.data?.enabled && location ? (
        <HassleLocationPicker
          key={locationKey(location)}
          location={location}
          busy={native.busy || config.isFetching || config.isError}
          onSaved={() => {
            void config.refetch();
          }}
        />
      ) : null}
    </SettingsSection>
  );
}
