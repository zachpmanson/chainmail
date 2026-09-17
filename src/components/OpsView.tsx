import { useNavigate, useSearch } from "@tanstack/react-router";
import { OpsMerges } from "./OpsMerges";
import { OpsOrgs } from "./OpsOrgs";
import { OpsPeople } from "./OpsPeople";
import { OPS_TABS, tabOf, type OpsTab } from "../lib/opsTabs";

/** The tab as the address writes it, labelled for the screen. */
const LABELS: Record<OpsTab, string> = {
  people: "People",
  orgs: "Organisations",
  merges: "Merges",
};

/**
 * The /ops route: one page per kind of thing the corpus is made of, because that
 * is what they are — people, the organisations their mail is drawn under, and the
 * merges the corpus proposes between them. Each tab owns its own read: the merge
 * plan is re-derived by walking the whole corpus, so it is fetched when the tab
 * that shows it is open rather than on every visit to the page.
 *
 * The tab lives in the address (?tab=merges), so a link can name one and a reload
 * lands on it. An unknown value opens the first tab rather than failing: a stale
 * link should still show the page.
 */
export function OpsView() {
  const { tab } = useSearch({ from: "/ops" });
  const navigate = useNavigate();
  // tabOf, not tab: the address is read here rather than trusted, so a stale
  // value opens the default tab instead of drawing none.
  const open: OpsTab = tabOf(tab);
  return (
    <div className="wrap opswrap">
      <div className="optabs" role="tablist" aria-label="What this page manages">
        {OPS_TABS.map((t) => (
          <button
            key={t}
            type="button"
            role="tab"
            id={`opstab-${t}`}
            aria-selected={t === open}
            aria-controls={`opspanel-${t}`}
            className={t === open ? "optab optab-on" : "optab"}
            title={`${LABELS[t]} — what the corpus knows about them`}
            onClick={() => void navigate({ to: "/ops", search: { tab: t } })}
          >
            {LABELS[t]}
          </button>
        ))}
      </div>
      <section
        id={`opspanel-${open}`}
        role="tabpanel"
        aria-labelledby={`opstab-${open}`}
        className="oppanel"
      >
        {open === "people" ? <OpsPeople /> : open === "orgs" ? <OpsOrgs /> : <OpsMerges />}
      </section>
    </div>
  );
}
