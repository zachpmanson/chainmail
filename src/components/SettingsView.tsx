import { useQueryClient } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { $api, type PersonSummary, type ServiceStatus, type Stats } from "../lib/api";
import { when } from "../lib/stamp";
import { useCompactMode } from "../lib/compactMode";
import { Palette } from "./Palette";
import { FolderPicker } from "./FolderPicker";
import { Button, ControlLink, SelectInput } from "./controls";

function errText(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

function SettingsSection({
  id,
  title,
  description,
  children,
}: {
  id: string;
  title: string;
  description: string;
  children: ReactNode;
}) {
  return (
    <section className="stsection min-w-0 rounded-[10px] border border-[var(--line)] bg-[var(--card)] px-4 py-[.95rem] max-[640px]:p-[.8rem]" aria-labelledby={id}>
      <header className="mb-[.7rem]">
        <h2 className="m-0 text-[.91rem] font-semibold tracking-[-.01em]" id={id}>{title}</h2>
        <p className="mt-1 text-[.74rem] leading-[1.45] text-[var(--muted)]">{description}</p>
      </header>
      {children}
    </section>
  );
}

function SettingRow({
  title,
  description,
  note,
  valueClassName,
  children,
}: {
  title: string;
  description: string;
  note?: ReactNode;
  valueClassName?: string;
  children: ReactNode;
}) {
  return (
    <div className="stsetting-row grid grid-cols-[minmax(0,1fr)_minmax(14rem,.9fr)] items-center gap-4 py-[.78rem] max-[640px]:grid-cols-1 max-[640px]:gap-[.55rem]">
      <div className="stsetting-copy min-w-0">
        <h3 className="m-0 text-[.78rem] font-semibold">{title}</h3>
        <p className="mt-[.2rem] text-[.72rem] leading-[1.4] text-[var(--muted)]">{description}</p>
      </div>
      <div className={`stsetting-value flex min-w-0 flex-col items-start gap-1${valueClassName ? ` ${valueClassName}` : ""}`}>
        {children}
        {note ? <p className="stsetting-note m-0 break-words text-[.7rem] leading-[1.4] text-[var(--muted)]">{note}</p> : null}
      </div>
    </div>
  );
}

function GmailAccounts() {
  const auth = $api.useQuery("get", "/auth/status", {});
  const disconnect = $api.useMutation("post", "/auth/accounts/{accountId}/disconnect", {
    onSuccess: () => auth.refetch(),
  });
  const connected = (auth.data?.accounts ?? []).filter((account) => account.signedIn);

  return (
    <SettingsSection
      id="gmail-accounts-heading"
      title="Gmail accounts"
      description="Connect the mailboxes Chainmail syncs. Disconnecting keeps already imported mail."
    >
      {auth.isError ? (
        <p className="stmessage-error mt-[.65rem] rounded-sm border-l-[3px] border-l-red-700 bg-[var(--bg)] px-[.65rem] py-2 text-[.76rem] leading-[1.45] text-[var(--fg)] break-words mb-0" role="alert">{errText(auth.error)}</p>
      ) : auth.isPending ? (
        <p className="mt-[.65rem] mb-0 text-[.76rem] leading-[1.45] text-[var(--muted)]">Checking connected accounts…</p>
      ) : (
        <>
          {connected.length > 0 ? (
            <ul className="mb-[.7rem] mt-0 list-none border-t border-[var(--line)] p-0">
              {connected.map((account) => {
                const label = account.email || account.displayName;
                return (
                  <li className="flex items-center justify-between gap-3 border-b border-[var(--line)] py-[.55rem] text-xs break-words" key={account.id}>
                    <span>{label}</span>
                    <Button
                      type="button"
                      variant="secondary"
                      aria-label={`Disconnect ${label}`}
                      disabled={disconnect.isPending}
                      onClick={() => {
                        const warning = account.id === "legacy"
                          ? " This also removes the shared Docket token."
                          : "";
                        if (window.confirm(`Disconnect ${label}? Chainmail will stop syncing from this mailbox, but imported mail stays in the corpus.${warning}`)) {
                          disconnect.mutate({ params: { path: { accountId: account.id } } });
                        }
                      }}
                    >
                      Disconnect
                    </Button>
                  </li>
                );
              })}
            </ul>
          ) : (
            <p className="mt-[.65rem] mb-0 text-[.76rem] leading-[1.45] text-[var(--muted)]">No Gmail accounts connected. Mailbox syncing is paused.</p>
          )}
          {disconnect.isError ? (
            <p className="mt-[.65rem] rounded-sm border-l-[3px] border-l-red-700 bg-[var(--bg)] px-[.65rem] py-2 text-[.76rem] leading-[1.45] text-[var(--fg)] break-words mb-0" role="alert">
              Could not disconnect: {errText(disconnect.error)}
            </p>
          ) : null}
          <ControlLink variant="primary" href="/auth/login">
            {connected.length === 0 ? "Sign in with Google" : "Connect another account"}
          </ControlLink>
        </>
      )}
    </SettingsSection>
  );
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
const BADGES: Record<ServiceStatus["status"], { word: string; cls: string }> = {
  ok: { word: "logged in", cls: "inline-flex whitespace-nowrap rounded-full border border-current px-[.48rem] py-[.13rem] text-[.64rem] font-bold text-green-800" },
  "needs-auth": { word: "needs auth", cls: "inline-flex whitespace-nowrap rounded-full border border-current px-[.48rem] py-[.13rem] text-[.64rem] font-bold text-amber-700" },
  down: { word: "down", cls: "inline-flex whitespace-nowrap rounded-full border border-current px-[.48rem] py-[.13rem] text-[.64rem] font-bold text-red-700" },
  unchecked: { word: "unchecked", cls: "inline-flex whitespace-nowrap rounded-full border border-current px-[.48rem] py-[.13rem] text-[.64rem] font-bold text-[var(--muted)]" },
};

function OneRow({ svc }: { svc: ServiceStatus }) {
  const badge = BADGES[svc.status] ?? BADGES.unchecked;
  return (
    <li className="flex flex-wrap items-center gap-x-[.7rem] gap-y-[.45rem] py-[.55rem]">
      <span className={badge.cls}>{badge.word}</span>
      <span className="min-w-32 text-[.8rem] font-semibold">{svc.label}</span>
      {svc.detail ? <span className="flex-[1_1_12rem] break-words text-[.72rem] text-[var(--muted)]">{svc.detail}</span> : null}
    </li>
  );
}

function CorpusStats({ s }: { s: Stats }) {
  const rows: [string, string][] = [
    ["entries", String(s.entries)],
    ["people", String(s.people)],
    ["thread roots", String(s.chainRoots)],
    ["unresolved", String(s.unresolved)],
  ];
  for (const [src, n] of Object.entries(s.bySource)) {
    rows.push([`${src} entries`, String(n)]);
  }
  for (const m of s.embeddings) {
    rows.push([`embeddings · ${m.model}`, `${m.vectors} vectors`]);
  }
  return (
    <dl className="mt-[.1rem] grid grid-cols-[repeat(auto-fit,minmax(9rem,1fr))] gap-2">
      {rows.map(([term, def]) => (
        <div className="min-w-0 rounded-lg border border-[var(--line)] bg-[var(--bg)] px-[.7rem] py-[.65rem]" key={term}>
          <dt className="break-words text-[.63rem] font-bold uppercase tracking-[.07em] text-[var(--muted)]">{term}</dt>
          <dd className="mt-[.3rem] break-words text-base font-semibold tabular-nums">{def}</dd>
        </div>
      ))}
    </dl>
  );
}

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
        <p className="mt-[.65rem] rounded-sm border-l-[3px] border-l-red-700 bg-[var(--bg)] px-[.65rem] py-2 text-[.76rem] leading-[1.45] text-[var(--fg)] break-words mb-0" role="alert">{errText(save.error)}</p>
      ) : null}

      <GmailAccounts />

      <SettingsSection
        id="services-heading"
        title="Connected services"
        description={`Run corpus status to refresh.${status.data?.checkedAt ? ` Last checked ${when(status.data.checkedAt)}.` : " Nothing measured yet."}`}
      >
        {status.isError ? (
          <p className="mt-[.65rem] rounded-sm border-l-[3px] border-l-red-700 bg-[var(--bg)] px-[.65rem] py-2 text-[.76rem] leading-[1.45] text-[var(--fg)] break-words mb-0" role="alert">{errText(status.error)}</p>
        ) : null}
        <ul className="mt-[.7rem] list-none border-t border-[var(--line)] p-0">
          {status.isPending ? (
            <li className="flex flex-wrap items-center gap-x-[.7rem] gap-y-[.45rem] py-[.55rem] text-[.78rem] text-[var(--muted)]">Checking services…</li>
          ) : status.data?.services.length ? (
            status.data.services.map((svc) => <OneRow key={svc.id} svc={svc} />)
          ) : (
            <li className="flex flex-wrap items-center gap-x-[.7rem] gap-y-[.45rem] py-[.55rem] text-[.78rem] text-[var(--muted)]">No services reported.</li>
          )}
        </ul>
      </SettingsSection>

      <SettingsSection
        id="mailbox-heading"
        title="Mailbox"
        description="Choose how often mail is imported and which folder opens first."
      >
        <div className="divide-y divide-[var(--line)] border-t border-[var(--line)]">
          <SettingRow
            title="Sync frequency"
            description="How often Chainmail checks Gmail for new messages."
            note={status.data?.nextSlurpAt
              ? `Next sweep ${when(status.data.nextSlurpAt)}.`
              : every === "off" ? "No automatic sweeps; refresh when needed."
                : every === "" ? "Reading the current schedule…" : "No automatic sweep is scheduled on this host."}
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
                <option key={word} value={word}>{label}</option>
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
              onPick={(name, accountId) => save.mutate({ body: {
                defaultFolder: name,
                defaultFolderAccountId: accountId ?? "",
              } })}
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
            note={!meKnown
              ? people.isError ? "Could not read the people in this corpus."
                : "Reading your identity…"
              : me.value === "" ? "Nobody is selected, so no messages are marked as yours."
                : `${meLine(me)}.`}
          >
            <SelectInput
              className="min-w-[14rem] max-w-96 cursor-pointer rounded-md border border-[var(--line)] bg-[var(--bg)] px-[.55rem] py-[.38rem] text-[.78rem] disabled:cursor-default disabled:opacity-60 max-[640px]:min-w-[min(100%,14rem)]"
              aria-label="Which person you are"
              value={me.value}
              disabled={busy || people.isPending}
              onChange={(e) => saveMe(e.target.value)}
            >
              {people.isPending ? null : me.options.map(([value, label]) => (
                <option key={value} value={value}>{label}</option>
              ))}
            </SelectInput>
          </SettingRow>
        </div>
        {people.isError ? (
          <p className="mt-[.65rem] rounded-sm border-l-[3px] border-l-red-700 bg-[var(--bg)] px-[.65rem] py-2 text-[.76rem] leading-[1.45] text-[var(--fg)] break-words mb-0" role="alert">{errText(people.error)}</p>
        ) : null}
      </SettingsSection>
      <SettingsSection
        id="corpus-heading"
        title="Corpus"
        description="A snapshot of the mail and identities currently stored."
      >
        {stats.isError ? (
          <p className="mt-[.65rem] rounded-sm border-l-[3px] border-l-red-700 bg-[var(--bg)] px-[.65rem] py-2 text-[.76rem] leading-[1.45] text-[var(--fg)] break-words mb-0" role="alert">{errText(stats.error)}</p>
        ) : stats.data ? (
          <CorpusStats s={stats.data} />
        ) : (
          <p className="mt-[.65rem] mb-0 text-[.76rem] leading-[1.45] text-[var(--muted)]">Reading the corpus…</p>
        )}
      </SettingsSection>

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