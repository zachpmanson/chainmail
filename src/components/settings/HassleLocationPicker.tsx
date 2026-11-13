import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { $api } from "../../lib/api/api";
import { canSelect } from "../../lib/hassles/native";
import { type HassleLocation } from "../../lib/hassles/forms";
import { errText } from "../../lib/ui/errText";
import Button from "../ui/Button";
import CheckboxRow from "../ui/CheckboxRow";
import FormField from "../ui/FormField";
import InlineAlert from "../ui/InlineAlert";
import SelectInput from "../ui/SelectInput";

export default function HassleLocationPicker({
  location,
  busy,
  onSaved,
}: {
  location: HassleLocation;
  busy: boolean;
  onSaved: () => void;
}) {
  const auth = $api.useQuery("get", "/auth/status", {});
  const [accountId, setAccount] = useState(location.accountId);
  const [calendarId, setCalendar] = useState("");
  const [dedicated, setDedicated] = useState(false);
  const calendars = $api.useQuery(
    "get",
    "/v1/hassles/calendars",
    { params: { query: { accountId } } },
    { enabled: !!accountId, retry: false, staleTime: 0 },
  );
  const client = useQueryClient();
  const save = $api.useMutation("post", "/v1/hassles/location", {
    retry: false,
    onSuccess: async (config) => {
      const queryKey = $api.queryOptions("get", "/v1/hassles/location", {}).queryKey;
      // Publish only the confirmed selection; an older settings read must not replace it.
      await client.cancelQueries({ queryKey, exact: true });
      client.setQueryData(queryKey, config);
    },
    onSettled: onSaved,
  });
  const disabled = busy || save.isPending;
  return (
    <section
      className="flex flex-col gap-3 rounded-md border border-line p-4"
      aria-label="Reminder location"
    >
      <p className="m-0 text-sm">
        Selected: {location.accountId || "none"} · {location.calendarId || "no calendar"}. Changing
        this selection never moves or deletes events.
      </p>
      <FormField label="Connected Google account" className="flex flex-col gap-1">
        <SelectInput
          value={accountId}
          disabled={disabled}
          onChange={(e) => {
            setAccount(e.target.value);
            setCalendar("");
            setDedicated(false);
          }}
        >
          <option value="">Choose an account…</option>
          {auth.data?.accounts
            .filter((a) => a.signedIn)
            .map((a) => (
              <option key={a.id} value={a.id}>
                {a.email || a.id}
              </option>
            ))}
        </SelectInput>
      </FormField>
      <FormField label="Dedicated reminder calendar" className="flex flex-col gap-1">
        <SelectInput
          value={calendarId}
          disabled={disabled || !accountId || calendars.isFetching || calendars.isError}
          onChange={(e) => {
            setCalendar(e.target.value);
            setDedicated(false);
          }}
        >
          <option value="">Choose a calendar…</option>
          {calendars.data?.calendars.map((c) => (
            <option key={c.id} value={c.id}>
              {c.name || c.id}
            </option>
          ))}
        </SelectInput>
      </FormField>
      <CheckboxRow
        checked={dedicated}
        disabled={disabled}
        onChange={(e) => setDedicated(e.target.checked)}
      >
        {" "}
        I acknowledge this is a dedicated reminders overlay: every event will be treated as a task.
      </CheckboxRow>
      <p className="m-0 text-xs text-muted">
        Discovery does not prove write permission. Choose a separate reminders calendar, not your
        primary meetings calendar. Non-docket events remain read-only.
      </p>
      <div>
        <Button
          disabled={
            disabled ||
            !calendarId ||
            !dedicated ||
            !calendars.data?.calendars.some((c) => c.id === calendarId)
          }
          onClick={() => {
            if (!canSelect(client)) return;
            save.mutate({
              body: { location: { accountId, calendarId }, previous: location, dedicated: true },
            });
          }}
        >
          Use calendar
        </Button>
      </div>
      {auth.isError || calendars.isError || save.isError ? (
        <InlineAlert>{errText(auth.error || calendars.error || save.error)}</InlineAlert>
      ) : null}
    </section>
  );
}
