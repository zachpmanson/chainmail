import { derive } from "../../lib/timeline/derive";
import type { Timeline as Spec } from "../../lib/timeline/spec";
import DiffPanel from "./DiffPanel";
import Legend from "./Legend";
import SourcesPanel from "./SourcesPanel";
import { type ThreadFilter } from "./Panels";
import ParticipantsPanel from "../thread/ParticipantsPanel";
import Minimap from "./Minimap";
import Chains from "./Chains";
import EntryBlock from "./EntryBlock";
import { html } from "../../lib/ui/html";

const toolbarButtonClasses =
  "inline-flex items-center rounded-md border border-line bg-card px-2 py-1 text-2xs font-bold uppercase tracking-[.08em] text-muted hover:border-accent hover:text-accent aria-pressed:border-accent aria-pressed:bg-mine aria-pressed:text-accent disabled:cursor-default disabled:opacity-55";

export default function Timeline({
  spec,
  marks,
  prevLabel,
  filter,
  onShowSpec,
  onRefresh,
  onAdd,
  onEval,
  onPull,
  pulling,
  mediaBase,
  refreshing,
}: {
  spec: Spec;
  /** entry id -> what changed since a previous render, from `--since` */
  marks?: Map<string, "new" | "revised">;
  prevLabel?: string;
  /** supplied by the app; absent in the static export, which cannot re-derive */
  filter?: ThreadFilter;
  /** app-only: the static export has no place to put an interactive panel */
  onShowSpec?: () => void;
  /** app-only: brings a saved page up to date from the corpus (refresh.go). */
  onRefresh?: () => void;
  /** app-only: opens a search to add another email's thread to this page. */
  onAdd?: () => void;
  /** app-only: opens the proposal evaluator, when the last refresh proposed chains. */
  onEval?: () => void;
  /** App-only, and only on a host started with -media. */
  onPull?: (extId: string) => void;
  /** the ext id whose files are being fetched, so its button says so and no second pull starts */
  pulling?: string | null;
  /** Absent in the static export, which has no server. */
  mediaBase?: string;
  refreshing?: boolean;
}) {
  const v = derive(spec);
  const s = v.spec;
  // gmailId -> first row holding it, so unspooled source lines can anchor on this page.
  const anchorByGmail = new Map<string, string>();
  for (const r of v.rows) {
    if (r.entry.gmailId && !anchorByGmail.has(r.entry.gmailId))
      anchorByGmail.set(r.entry.gmailId, r.id);
  }
  return (
    <>
      {v.avatarCss ? <style dangerouslySetInnerHTML={html(v.avatarCss)} /> : null}
      <div className="fixed top-2 right-[calc(var(--panel)+.6rem)] z-31 flex gap-1.5 max-[1024px]:right-[.6rem] print:hidden">
        <button
          className={toolbarButtonClasses}
          id="viewtog"
          type="button"
          aria-pressed="false"
          aria-label="Thread columns view"
        >
          columns
        </button>
        {onShowSpec ? (
          <button
            className={toolbarButtonClasses}
            id="spectog"
            type="button"
            onClick={onShowSpec}
            aria-label="Show the spec as JSON"
          >
            json
          </button>
        ) : null}
        {onRefresh ? (
          <button
            className={toolbarButtonClasses}
            id="refreshtog"
            type="button"
            onClick={onRefresh}
            disabled={refreshing}
            aria-label="Re-derive this page from the corpus"
          >
            {refreshing ? "refreshing…" : "refresh"}
          </button>
        ) : null}
        {onAdd ? (
          <button
            className={toolbarButtonClasses}
            type="button"
            onClick={onAdd}
            aria-label="Search the corpus for another email to add to this page"
          >
            add email
          </button>
        ) : null}
        {onEval ? (
          <button
            className={toolbarButtonClasses}
            type="button"
            onClick={onEval}
            aria-label="Evaluate chains the queries proposed"
          >
            eval
          </button>
        ) : null}
        <button
          className={toolbarButtonClasses}
          id="maptog"
          type="button"
          aria-pressed="true"
          aria-label="Reply tree panel"
        >
          tree
        </button>
        <button
          className={toolbarButtonClasses}
          id="plaintog"
          type="button"
          aria-pressed="false"
          aria-label="Ignore the sender's own formatting"
        >
          plain
        </button>
      </div>
      <div className="wrap mx-auto max-w-[76rem] px-5 pt-7 pb-14">
        <header className="top mb-1 border-b border-line pb-3">
          <h1 className="m-0 mb-1 text-xl tracking-[-.01em]">
            {v.hashed ? <span className="font-normal text-muted">#</span> : null}
            {v.hashed ? v.title.slice(1) : v.title}
          </h1>
          <p
            className="mb-2 text-sm text-muted"
            dangerouslySetInnerHTML={html(s.subtitle ?? `${s.messages.length} messages.`)}
          />
          <Legend />
          <ParticipantsPanel v={v} open />
          {marks ? (
            <DiffPanel v={v} marks={marks} prevLabel={prevLabel ?? "the previous run"} />
          ) : null}
          <SourcesPanel v={v} filter={filter} />
        </header>
        <div className="stream" id="stream" style={{ ["--nch" as string]: v.layout.laneCount }}>
          <Chains v={v} />
          {v.rows.map((r) => (
            <EntryBlock
              key={r.id}
              row={r}
              v={v}
              mark={marks?.get(r.id)}
              anchorByGmail={anchorByGmail}
              onPull={onPull}
              pulling={pulling}
              mediaBase={mediaBase}
            />
          ))}
        </div>
        {s.openItems?.length ? (
          <footer className="end mt-8 border-t border-line pt-4">
            <h2 className="mt-0 mb-2 text-sm tracking-[.1em] text-muted uppercase">
              {s.openItemsTitle ?? "Still open"}
            </h2>
            <ul className="m-0 pl-5">
              {s.openItems.map((i, n) => (
                <li className="my-1 text-sm" key={n} dangerouslySetInnerHTML={html(i)} />
              ))}
            </ul>
          </footer>
        ) : null}
      </div>
      <Minimap v={v} />
    </>
  );
}
