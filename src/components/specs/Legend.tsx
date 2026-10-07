/** The four bubble states, so the page explains its own notation. */
export default function Legend() {
  return (
    <div className="mt-2 grid grid-cols-[repeat(auto-fit,minmax(17rem,1fr))] gap-x-4 gap-y-1.5 text-xs text-muted">
      <div className="flex items-start gap-2 leading-snug">
        <span className="plain mt-px size-[1.15rem] shrink-0 rounded-md border border-line bg-card" />
        <div>
          <b className="font-semibold text-fg">Solid</b> — a real standalone message in the mailbox.
          The caret on its header opens the ids it was found under, its Gmail message&nbsp;id among
          them.
        </div>
      </div>
      <div className="flex items-start gap-2 leading-snug">
        <span className="mt-px size-[1.15rem] shrink-0 rounded-md border border-dashed border-muted/55 bg-dash" />
        <div>
          <b className="font-semibold text-fg">Dashed</b> — reconstructed from quoted text inside a
          later email; no message of its own. Its header names the email it came out of, and its
          timestamp is the one in the quoted header.
        </div>
      </div>
      <div className="flex items-start gap-2 leading-snug">
        <span className="mt-px size-[1.15rem] shrink-0 rounded-md border border-org-3 bg-mine" />
        <div>
          <b className="font-semibold text-fg">Tinted</b> — sent by you.
        </div>
      </div>
      <div className="flex items-start gap-2 leading-snug">
        <span className="flex size-[1.15rem] shrink-0 items-center justify-center text-sm">
          &#128206;
        </span>
        <div>
          <b className="font-semibold text-fg">Attachment</b> — links through to that message in
          Gmail. Only detectable on real mailbox messages, never on reconstructed ones.
        </div>
      </div>
    </div>
  );
}
