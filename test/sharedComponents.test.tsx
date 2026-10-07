import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { Avatar } from "../src/components/thread/Avatar";
import { Checkbox, CheckboxRow } from "../src/components/ui/Checkbox";
import { DialogShell } from "../src/components/ui/DialogShell";
import { FormField } from "../src/components/ui/FormField";
import { InlineAlert } from "../src/components/ui/InlineAlert";
import { ThreadCounts } from "../src/components/inbox/ThreadRow";
import { StatusBadge } from "../src/components/ui/StatusBadge";

describe("shared UI primitives", () => {
  it("keeps the shared avatar's image and initials variants", () => {
    const initials = renderToStaticMarkup(
      <Avatar name="Ada Byron" orgSlot="o2" size="participant" />,
    );
    const picture = renderToStaticMarkup(
      <Avatar name="Ada Byron" orgSlot="o2" pic="p0" title="Ada Byron <ada@example.test>" />,
    );
    expect(initials).toContain('class="av o2 bg-org-2 [--ring:var(--o2)] grid');
    expect(initials).toContain("AB");
    expect(initials).toContain("text-[.78rem]");
    expect(picture).toContain("av o2 bg-org-2 [--ring:var(--o2)] pic p0 bg-cover bg-center");
    expect(picture).not.toContain("ini ");
    expect(picture).toContain('title="Ada Byron &lt;ada@example.test&gt;"');
  });

  it("offers a labeled checkbox row and a bare checkbox with chosen accent", () => {
    const row = renderToStaticMarkup(
      <CheckboxRow
        checked
        accent="accent"
        className="flex gap-2"
        title="toggle"
        onChange={() => {}}
      >
        reply all
      </CheckboxRow>,
    );
    const bare = renderToStaticMarkup(
      <Checkbox checked accent="org" aria-label="select" onChange={() => {}} />,
    );
    expect(row).toContain('<label class="flex gap-2" title="toggle">');
    expect(row).toContain('type="checkbox"');
    expect(row).toContain('checked=""');
    expect(row).toContain('class="accent-accent"');
    expect(row).toContain("reply all</label>");
    expect(bare).toContain("accent-org-1");
    expect(bare).toContain('aria-label="select"');
  });

  it("shares status tones without changing feature-specific hooks", () => {
    const badge = renderToStaticMarkup(
      <StatusBadge tone="success" className="opbad op-apply">
        yours
      </StatusBadge>,
    );
    expect(badge).toContain("opbad op-apply");
    expect(badge).toContain("text-green-800");
    expect(badge).toContain(">yours</span>");
  });

  it("frames dialogs but leaves dismissal policy to the owner", () => {
    const dialog = renderToStaticMarkup(
      <DialogShell label="Confirm">
        <p>Proceed?</p>
      </DialogShell>,
    );
    expect(dialog).toContain('role="dialog" aria-modal="true" aria-label="Confirm"');
    expect(dialog).toContain("proposals-panel flex max-h-[70vh]");
    expect(dialog).toContain("<p>Proceed?</p>");
  });

  it("shares thread-row count thresholds and marks across row layouts", () => {
    const populated = renderToStaticMarkup(
      <ThreadCounts people={3} entries={2} attachments={1} className="group-hover:invisible" />,
    );
    expect(populated).toContain(
      'class="ibtail ml-auto flex shrink-0 items-center gap-2 group-hover:invisible"',
    );
    expect(populated).toContain('title="3 people in this thread — senders and recipients"');
    expect(populated).toContain('title="2 messages in this thread"');
    expect(populated).toContain('title="1 attachment in this thread"');

    const belowThreshold = renderToStaticMarkup(
      <ThreadCounts people={2} entries={1} attachments={0} />,
    );
    expect(belowThreshold).not.toContain("people in this thread");
    expect(belowThreshold).not.toContain("messages in this thread");
    expect(belowThreshold).not.toContain("attachment in this thread");
  });

  it("shares the inline alert surface and keeps caller-provided message markup", () => {
    const alert = renderToStaticMarkup(
      <InlineAlert>
        <strong>Could not save</strong> Try again.
      </InlineAlert>,
    );
    expect(alert).toContain(
      'class="selfail mt-[.7rem] rounded-md border border-line border-l-[3px] border-l-red-700 bg-card px-[.7rem] py-2 text-[.82rem]"',
    );
    expect(alert).toContain('role="alert"');
    expect(alert).toContain("<strong>Could not save</strong> Try again.");
  });

  it("associates field text with its control", () => {
    const field = renderToStaticMarkup(
      <FormField className="flex gap-2" label="Search" labelClassName="text-muted">
        <input type="search" />
      </FormField>,
    );
    expect(field).toContain('<label class="flex gap-2">');
    expect(field).toContain('<span class="text-muted">Search</span>');
    expect(field).toContain('<input type="search"/>');
  });
});
