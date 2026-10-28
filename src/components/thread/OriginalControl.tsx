import type { Original } from "../../lib/message/useOriginal";
import type { ReadingPreference } from "../../lib/prefs/usePrefs";
import SelectInput from "../ui/SelectInput";

/** Preference mode stays distinct from the rendering Default happens to select. */
export default function OriginalControl({
  preference,
  state,
  choose,
  pending = false,
}: {
  preference: ReadingPreference;
  state: Original;
  choose: (next: ReadingPreference) => void;
  pending?: boolean;
}) {
  return (
    <>
      <SelectInput
        className="min-h-0 border-transparent bg-transparent px-1 py-0.5 text-2xs text-muted"
        aria-label="Reading style for this sender"
        aria-busy={pending || state.at === "asking"}
        disabled={pending}
        title={
          pending ? "Saving reading style…" : "Default follows automatic rendering for each message"
        }
        value={preference === undefined ? "default" : preference ? "original" : "simplified"}
        onChange={(event) =>
          choose(event.target.value === "default" ? undefined : event.target.value === "original")
        }
      >
        <option value="default">Default</option>
        <option value="original">Original</option>
        <option value="simplified">Simplified</option>
      </SelectInput>
      {state.at === "none" ? (
        <span className="text-2xs text-muted italic" title={state.why}>
          nothing to show
        </span>
      ) : null}
    </>
  );
}
