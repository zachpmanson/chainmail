import { useQueryClient } from "@tanstack/react-query";
import { $api, type PersonSummary } from "../../lib/api/api";
import { invalidateChains } from "../../lib/api/queryKeys";
import { when } from "../../lib/ui/stamp";
import { usePrefs } from "../../lib/prefs/usePrefs";
import Palette from "./Palette";
import FolderPicker from "../inbox/FolderPicker";
import SelectInput from "../ui/SelectInput";
import InlineAlert from "../ui/InlineAlert";
import SettingsAccounts from "./SettingsAccounts";
import SettingsCorpus from "./SettingsCorpus";
import SettingsServices from "./SettingsServices";
import SettingRow from "./SettingRow";
import SettingsSection from "./SettingsSection";
import { errText } from "../../lib/ui/errText";

/** The server's canonical cadence words (cmd/server/schedule.go). */
const CADENCES: [string, string][] = [
  ["5m", "5 minutes"],
  ["10m", "10 minutes"],
  ["15m", "15 minutes"],
  ["30m", "30 minutes"],
  ["1h", "every hour"],
  ["6h", "every 6 hours"],
  ["off", "never"],
];

/** The server accepts any whole-minute cadence, so keep an unlisted value rather
 *  than snapping to the first option and writing that back. */
function cadenceOptions(every: string): [string, string][] {
  return CADENCES.some(([word]) => word === every) ? CADENCES : [...CADENCES, [every, every]];
}

function emailIdentities(p: PersonSummary): string[] {
  return (p.identities ?? [])
    .filter((i) => i.startsWith("email:"))
    .map((i) => i.slice("email:".length));
}

/** Stands for a legacy addresses-only setting; never sent to the server. */
const STORED_ADDRESSES = "addresses";

/** Only people with an email address are offered; the API refuses the rest. */
function meOptions(
  people: PersonSummary[],
  personId: number | undefined,
  addresses: string[],
): { options: [string, string][]; value: string; person: string; addresses: string[] } {
  const options: [string, string][] = [["", "Nobody"]];
  let person = "";
  for (const p of people) {
    if (emailIdentities(p).length === 0) continue;
    options.push([String(p.personId), p.displayName]);
    if (p.personId === personId) person = p.displayName;
  }
  let value = personId === undefined ? "" : String(personId);
  if (value === "" && addresses.length > 0) {
    value = STORED_ADDRESSES;
    options.push([STORED_ADDRESSES, addresses.join(", ")]);
  }
  return { options, value, person, addresses };
}

function meLine(me: { person: string; addresses: string[] }): string {
  return [me.person, me.addresses.join(", ")].filter((s) => s !== "").join(": ");
}

export default function SettingsView() {
  const queryClient = useQueryClient();
  const compact = usePrefs((s) => s.compact);
  const setCompact = usePrefs((s) => s.setCompact);
  const status = $api.useQuery("get", "/v1/status", {});
  const stats = $api.useQuery("get", "/v1/stats", {});
  const settings = $api.useQuery("get", "/v1/settings", {});
  const save = $api.useMutation("post", "/v1/settings", {
    onSuccess: (_stored, variables) => {
      void settings.refetch();
      // The next sweep is derived from the cadence.
      void status.refetch();
      // The pane's marks come from the stored person, so redraw the open thread.
      if (variables.body?.mePersonId !== undefined) {
        void invalidateChains(queryClient);
      }
    },
  });
  // Send the person id, not addresses, so aliases stay the identity graph's call.
  // Omitted fields are left unchanged; 0 clears.
  const saveMe = (picked: string) => save.mutate({ body: { mePersonId: Number(picked) } });
  const people = $api.useQuery("get", "/v1/people", {});
  const me = meOptions(
    people.data?.people ?? [],
    settings.data?.mePersonId,
    settings.data?.me ?? [],
  );
  const meKnown = !settings.isPending && !people.isPending;
  const every = settings.data?.slurpEvery ?? "";
  const folder = settings.data?.defaultFolder ?? "";
  const busy = save.isPending || settings.isPending;

  return (
    <div className="wrap mx-auto flex w-[min(calc(100%-2rem),58rem)] max-w-[58rem] flex-col gap-4 px-0 py-5 max-[640px]:w-[calc(100%-.5rem)] max-[640px]:pt-3">
      <header className="mx-0.5 my-1 mb-1.5">
        <h1 className="m-0 text-2xl">Settings</h1>
      </header>

      {save.isError ? <InlineAlert compact>{errText(save.error)}</InlineAlert> : null}

      <SettingsAccounts />
      <SettingsServices status={status} />

      <SettingsSection
        id="mailbox-heading"
        title="Mailbox"
        description="Choose how often mail is imported and which folder opens first."
      >
        <div className="divide-y divide-line border-t border-line">
          <SettingRow
            title="Sync frequency"
            description="How often Chainmail checks Gmail for new messages."
            note={
              status.data?.nextSlurpAt
                ? `Next sweep ${when(status.data.nextSlurpAt)}.`
                : every === "off"
                  ? "No automatic sweeps; refresh when needed."
                  : every === ""
                    ? "Reading the current schedule…"
                    : "No automatic sweep is scheduled on this host."
            }
          >
            <SelectInput
              className="max-w-full min-w-56 cursor-pointer rounded-md border border-line bg-bg px-2 py-1.5 text-xs disabled:cursor-default disabled:opacity-60 max-[640px]:min-w-[min(100%,14rem)]"
              aria-label="How often to sweep the mailbox"
              value={every}
              disabled={busy || every === ""}
              onChange={(e) => save.mutate({ body: { slurpEvery: e.target.value } })}
            >
              {every === "" ? <option value="">…</option> : null}
              {cadenceOptions(every).map(([word, label]) => (
                <option key={word} value={word}>
                  {label}
                </option>
              ))}
            </SelectInput>
          </SettingRow>
          <SettingRow
            title="Home folder"
            description="The folder shown when you open the inbox."
            valueClassName="w-full items-stretch"
          >
            <FolderPicker
              current={folder}
              currentAccountId={settings.data?.defaultFolderAccountId}
              mode="default"
              ariaLabel="Which folder the home page opens in"
              disabled={busy}
              onPick={(name, accountId) =>
                save.mutate({
                  body: {
                    defaultFolder: name,
                    defaultFolderAccountId: accountId ?? "",
                  },
                })
              }
            />
          </SettingRow>
        </div>
      </SettingsSection>

      <SettingsSection
        id="reading-heading"
        title="Reading"
        description="Personalize how the inbox and message threads are presented."
      >
        <div>
          <SettingRow
            title="Thread list"
            description="Choose how much detail each inbox row shows."
            note={compact ? "One line per thread." : "Sender, subject, and preview."}
          >
            <SelectInput
              className="max-w-full min-w-56 cursor-pointer rounded-md border border-line bg-bg px-2 py-1.5 text-xs disabled:cursor-default disabled:opacity-60 max-[640px]:min-w-[min(100%,14rem)]"
              aria-label="Thread list layout"
              value={compact ? "compact" : "detailed"}
              onChange={(e) => setCompact(e.target.value === "compact")}
            >
              <option value="detailed">Detailed</option>
              <option value="compact">Compact</option>
            </SelectInput>
          </SettingRow>
          <SettingRow
            title="Your mail comes from"
            description="Choose the person whose messages should be marked as yours."
            note={
              !meKnown
                ? people.isError
                  ? "Could not read the people in this corpus."
                  : "Reading your identity…"
                : me.value === ""
                  ? "Nobody is selected, so no messages are marked as yours."
                  : `${meLine(me)}.`
            }
          >
            <SelectInput
              className="max-w-96 min-w-56 cursor-pointer rounded-md border border-line bg-bg px-2 py-1.5 text-xs disabled:cursor-default disabled:opacity-60 max-[640px]:min-w-[min(100%,14rem)]"
              aria-label="Which person you are"
              value={me.value}
              disabled={busy || people.isPending}
              onChange={(e) => saveMe(e.target.value)}
            >
              {people.isPending
                ? null
                : me.options.map(([value, label]) => (
                    <option key={value} value={value}>
                      {label}
                    </option>
                  ))}
            </SelectInput>
          </SettingRow>
        </div>
        {people.isError ? <InlineAlert compact>{errText(people.error)}</InlineAlert> : null}
      </SettingsSection>
      <SettingsCorpus stats={stats} />

      <SettingsSection
        id="appearance-heading"
        title="Appearance"
        description="Inspect the colours used by Chainmail in both themes."
      >
        <Palette />
      </SettingsSection>
    </div>
  );
}
