import { useQueryClient } from "@tanstack/react-query";
import { $api, type PersonSummary } from "../../lib/api";
import { when } from "../../lib/stamp";
import { useCompactMode } from "../../lib/compactMode";
import { Palette } from "./Palette";
import { FolderPicker } from "../inbox/FolderPicker";
import { SelectInput } from "../ui/controls";
import { SettingsAccounts } from "./SettingsAccounts";
import { SettingsCorpus, SettingsServices } from "./SettingsOverview";
import { SettingRow, SettingsSection } from "./SettingsScaffold";

function errText(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

/**
 * The cadences the sweep control offers, as the words the server stores and
 * serves them in (cmd/server/schedule.go). One vocabulary, listed once: a
 * control whose options the server canonicalises differently would be handing
 * back a value it then could not show.
 *
 * "Never" is a cadence rather than the absence of one: the mailbox is then read
 * only when someone asks for it, which is a choice a reader can want and the one
 * the refresh button already offers.
 */
const CADENCES: [string, string][] = [
  ["5m", "5 minutes"],
  ["10m", "10 minutes"],
  ["15m", "15 minutes"],
  ["30m", "30 minutes"],
  ["1h", "every hour"],
  ["6h", "every 6 hours"],
  ["off", "never"],
];

/**
 * The options, with the value in force appended when it is not one of them. The
 * server takes any whole-minute cadence between a minute and a day, so one set
 * through the API has to leave the control able to show it rather than silently
 * snapping to the first option and writing that back.
 */
function cadenceOptions(every: string): [string, string][] {
  return CADENCES.some(([word]) => word === every) ? CADENCES : [...CADENCES, [every, every]];
}

/**
 * The addresses a person is, out of the "kind:value" identities the corpus
 * serves. Only `email:` — a Slack uid is not a mailbox a message can be marked
 * as coming from, and a display name borrowed from somebody else's quote is a
 * placeholder for a person nobody has identified yet.
 */
function emailIdentities(p: PersonSummary): string[] {
  return (p.identities ?? [])
    .filter((i) => i.startsWith("email:"))
    .map((i) => i.slice("email:".length));
}

/**
 * The value the reader control shows for a corpus whose setting is the address
 * list it was configured with before this control named a person. It is not a
 * person id and is never sent anywhere: a change away from it writes whichever
 * person was picked, so the state is shown honestly and left behind in one click.
 */
const STORED_ADDRESSES = "addresses";

/**
 * Who the reader is, as the options of the one control that says so, and which of
 * them the setting names.
 *
 * A person rather than a field of addresses, because the addresses are the
 * corpus's to fold and not the reader's to retype: one account behind
 * zach@loomworks.example and zach@millrace.example is one person in the identity graph,
 * and a field asked the reader to do that folding by hand, again for every alias
 * they would rather their own mail were marked from.
 *
 * Only people the corpus has an address for are offered — a person known only by
 * a display name recovered from somebody else's quote has no mailbox for mail to
 * have come from, and the API refuses one too — and they come in the corpus's own
 * order, most involved first, which is where the reader of their own corpus is.
 *
 * The value is the id, and the addresses in force come back from the server
 * resolved: what a control wrote is the person, and which aliases that is today is
 * the corpus's answer rather than the page's (see the sentence under it).
 */
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
    // The address list a corpus was configured with before the setting was a
    // person: shown as the value it is rather than as nobody, and appended the
    // way the folder and cadence controls keep a value they cannot name.
    value = STORED_ADDRESSES;
    options.push([STORED_ADDRESSES, addresses.join(", ")]);
  }
  return { options, value, person, addresses };
}

/**
 * The addresses being marked, under the name of the person they are: "Ada Byron:
 * ada@loomworks.example". Either half can be missing — a corpus configured before
 * this control had a person to name has only addresses, and the two fields arrive
 * from two different answers — so the line is built from what is there rather
 * than from a template that would print a colon with nothing after it. What is
 * left is still the answer to whose mail this is.
 */
function meLine(me: { person: string; addresses: string[] }): string {
  return [me.person, me.addresses.join(", ")].filter((s) => s !== "").join(": ");
}

/**
 * A badge's wording and colour, per the state the probe reported. A state is
 * a truth the screen is asserting, so it earns a colour; "unchecked" is the
 * calm first-boot grey rather than an error, because nothing has been asked
 * yet and the answer is not "no".
 */
/**
 * Account management, service health, mailbox preferences, reading options,
 * and a compact view of the corpus, grouped into consistently framed sections.
 */
export function SettingsView() {
  const queryClient = useQueryClient();
  const [compact, setCompact] = useCompactMode();
  const status = $api.useQuery("get", "/v1/status", {});
  const stats = $api.useQuery("get", "/v1/stats", {});
  const settings = $api.useQuery("get", "/v1/settings", {});
  // The settings on this screen, written the way the home page writes its own:
  // only the field being changed is named, and the rest are left as they stand.
  const save = $api.useMutation("post", "/v1/settings", {
    onSuccess: (_stored, variables) => {
      void settings.refetch();
      // The next sweep is computed from the cadence, so a change to one is a
      // change to the other: without this the line under the control would keep
      // naming the old schedule until the page was reloaded.
      void status.refetch();
      // The pane's marks are resolved from the stored person, so a write that
      // names them has to redraw the open thread: without this the reader says
      // who they are and the thread they are looking at keeps showing their own
      // mail unmarked until they reload. The other settings change nothing the
      // pane draws, and re-reading a thread for one would be a request per click
      // on a control that has nothing to do with threads.
      if (variables.body?.mePersonId !== undefined) {
        void queryClient.invalidateQueries({ queryKey: ["get", "/v1/chains/{rootExtId}"] });
      }
    },
  });
  // Persist who the reader is, as the person the control offered. What travels is
  // the person's id and not their addresses: which addresses are one human is the
  // identity graph's reading, and a client that sent a list would be storing a
  // second one that goes stale the moment an alias is learned. Only `mePersonId`
  // is named in the body, which is what leaves the cadence and the folder as they
  // stand, and "Nobody" is a zero — which is not a person id, so clearing is the
  // same field with the same meaning.
  const saveMe = (picked: string) => save.mutate({ body: { mePersonId: Number(picked) } });
  // The people the corpus holds, which is where the reader picks themselves: the
  // identity graph is the only thing in corpus that knows two addresses are one
  // person, and the setting is that question asked once.
  const people = $api.useQuery("get", "/v1/people", {});
  const me = meOptions(
    people.data?.people ?? [],
    settings.data?.mePersonId,
    settings.data?.me ?? [],
  );
  // Which the sentence under the control can be written from yet: the settings are
  // what say who is in force and which addresses that means, and the people are
  // what names them, so until both have answered the line says so rather than
  // calling the reader nobody.
  const meKnown = !settings.isPending && !people.isPending;
  const every = settings.data?.slurpEvery ?? "";
  const folder = settings.data?.defaultFolder ?? "";
  const busy = save.isPending || settings.isPending;

  return (
    <div className="wrap mx-auto grid w-[min(calc(100%-2rem),58rem)] max-w-[58rem] content-start gap-[.9rem] px-0 py-5 max-[640px]:w-[calc(100%-.5rem)] max-[640px]:pt-3">
      <header className="my-[.2rem] mb-[.35rem] mx-[.15rem]">
        <h1 className="m-0 text-[1.45rem]">Settings</h1>
      </header>

      {save.isError ? (
        <p
          className="mt-[.65rem] rounded-sm border-l-[3px] border-l-red-700 bg-[var(--bg)] px-[.65rem] py-2 text-[.76rem] leading-[1.45] text-[var(--fg)] break-words mb-0"
          role="alert"
        >
          {errText(save.error)}
        </p>
      ) : null}

      <SettingsAccounts />
      <SettingsServices status={status} />

      <SettingsSection
        id="mailbox-heading"
        title="Mailbox"
        description="Choose how often mail is imported and which folder opens first."
      >
        <div className="divide-y divide-[var(--line)] border-t border-[var(--line)]">
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
              className="min-w-[14rem] max-w-full cursor-pointer rounded-md border border-[var(--line)] bg-[var(--bg)] px-[.55rem] py-[.38rem] text-[.78rem] disabled:cursor-default disabled:opacity-60 max-[640px]:min-w-[min(100%,14rem)]"
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
            valueClassName="stsetting-value-fill w-full items-stretch"
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
        <div className="stsetting-list">
          <SettingRow
            title="Thread list"
            description="Choose how much detail each inbox row shows."
            note={compact ? "One line per thread." : "Sender, subject, and preview."}
          >
            <SelectInput
              className="min-w-[14rem] max-w-full cursor-pointer rounded-md border border-[var(--line)] bg-[var(--bg)] px-[.55rem] py-[.38rem] text-[.78rem] disabled:cursor-default disabled:opacity-60 max-[640px]:min-w-[min(100%,14rem)]"
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
              className="min-w-[14rem] max-w-96 cursor-pointer rounded-md border border-[var(--line)] bg-[var(--bg)] px-[.55rem] py-[.38rem] text-[.78rem] disabled:cursor-default disabled:opacity-60 max-[640px]:min-w-[min(100%,14rem)]"
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
        {people.isError ? (
          <p
            className="mt-[.65rem] rounded-sm border-l-[3px] border-l-red-700 bg-[var(--bg)] px-[.65rem] py-2 text-[.76rem] leading-[1.45] text-[var(--fg)] break-words mb-0"
            role="alert"
          >
            {errText(people.error)}
          </p>
        ) : null}
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
