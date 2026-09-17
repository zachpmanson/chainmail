/** The subjects /ops manages, in the order the tabs are drawn: the corpus's own
 *  rows first, then what its mail is drawn as, then the merges proposed between
 *  them. Order matters only to the drawing — the first is the default tab. */
export const OPS_TABS = ["people", "orgs", "merges"] as const;

export type OpsTab = (typeof OPS_TABS)[number];

/**
 * The tab a value names. Anything else — a value this build does not know, a
 * typo, an old link — is the default tab rather than an error: an address is not
 * a request that can be refused, and the page it names is this one.
 *
 * Deliberately the one place the rule is written. The route validates with it and
 * the page reads with it, because a router does not necessarily run the validator
 * over an address it was handed at load, and a page that trusted it to would draw
 * no tab at all for a stale link.
 */
export function tabOf(value: unknown): OpsTab {
  const tab = String(value ?? "");
  return (OPS_TABS as readonly string[]).includes(tab) ? (tab as OpsTab) : OPS_TABS[0];
}

/** The ops tab, read from the address. */
export function validateOpsTab(search: Record<string, unknown>): { tab?: OpsTab } {
  return search.tab === undefined ? {} : { tab: tabOf(search.tab) };
}
