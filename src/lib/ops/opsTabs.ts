/** In tab order; the first is the default. */
export const OPS_TABS = ["people", "orgs", "merges"] as const;

export type OpsTab = (typeof OPS_TABS)[number];

/** Unknown values fall back to the default tab. The page calls this too: the
 *  router may skip validation on initial load. */
export function tabOf(value: unknown): OpsTab {
  const tab = String(value ?? "");
  return (OPS_TABS as readonly string[]).includes(tab) ? (tab as OpsTab) : OPS_TABS[0];
}

/** The ops tab, read from the address. */
export function validateOpsTab(search: Record<string, unknown>): { tab?: OpsTab } {
  return search.tab === undefined ? {} : { tab: tabOf(search.tab) };
}
