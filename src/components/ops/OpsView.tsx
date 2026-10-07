import { useNavigate, useSearch } from "@tanstack/react-router";
import OpsMerges from "./OpsMerges";
import OpsOrgs from "./OpsOrgs";
import OpsPeople from "./OpsPeople";
import { OPS_TABS, tabOf, type OpsTab } from "../../lib/ops/opsTabs";
import Button from "../ui/Button";

const LABELS: Record<OpsTab, string> = {
  people: "People",
  orgs: "Organisations",
  merges: "Merges",
};

export default function OpsView() {
  const { tab } = useSearch({ from: "/ops" });
  const navigate = useNavigate();
  const open: OpsTab = tabOf(tab);
  return (
    <div className="wrap mx-0 w-full max-w-none px-5 pt-7 pb-14">
      <div
        className="mt-2 mb-0.5 flex items-stretch gap-0.5 border-b border-line"
        role="tablist"
        aria-label="What this page manages"
      >
        {OPS_TABS.map((t) => (
          <Button
            key={t}
            variant="bare"
            type="button"
            role="tab"
            id={`opstab-${t}`}
            aria-selected={t === open}
            aria-controls={`opspanel-${t}`}
            className="inline-flex items-center justify-center gap-2 border-b-2 border-transparent px-3 py-2 text-xs font-bold tracking-[.08em] text-muted transition-colors hover:border-line hover:bg-card hover:text-accent aria-selected:border-accent aria-selected:text-fg"
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
        className="mt-0.5"
      >
        {open === "people" ? <OpsPeople /> : open === "orgs" ? <OpsOrgs /> : <OpsMerges />}
      </section>
    </div>
  );
}
