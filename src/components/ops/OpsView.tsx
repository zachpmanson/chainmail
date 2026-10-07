import { useNavigate, useSearch } from "@tanstack/react-router";
import { OpsMerges } from "./OpsMerges";
import { OpsOrgs } from "./OpsOrgs";
import { OpsPeople } from "./OpsPeople";
import { OPS_TABS, tabOf, type OpsTab } from "../../lib/opsTabs";
import { Button } from "../ui/controls";

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
    <div className="wrap opswrap mx-0 w-full max-w-none px-5 pt-7 pb-14">
      <div
        className="mt-2 mb-[.1rem] flex items-stretch gap-[.15rem] border-b border-line"
        role="tablist"
        aria-label="What this page manages"
      >
        {OPS_TABS.map((t) => (
          <Button
            key={t}
            variant="quiet"
            type="button"
            role="tab"
            id={`opstab-${t}`}
            aria-selected={t === open}
            aria-controls={`opspanel-${t}`}
            className="min-h-0 rounded-none border-0 border-b-2 border-transparent px-[.7rem] py-[.45rem] text-[.7rem] font-bold tracking-[.08em] text-muted aria-selected:border-accent aria-selected:text-fg"
            title={`${LABELS[t]} — what the corpus knows about them`}
            onClick={() => void navigate({ to: "/ops", search: { tab: t } })}
          >
            {LABELS[t]}
          </Button>
        ))}
      </div>
      <section
        id={`opspanel-${open}`}
        role="tabpanel"
        aria-labelledby={`opstab-${open}`}
        className="mt-[.15rem]"
      >
        {open === "people" ? <OpsPeople /> : open === "orgs" ? <OpsOrgs /> : <OpsMerges />}
      </section>
    </div>
  );
}
