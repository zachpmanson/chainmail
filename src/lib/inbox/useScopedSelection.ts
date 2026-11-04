import { useCallback, useState } from "react";
import { toggleRow, toggleShown, type RowSelection } from "./rangeSelection";

type ScopedSelection = RowSelection & { scope: string; shown: readonly string[]; owner: object };

/** Clear ticks before rendering a new scope, without remounting its reader or composer. */
export default function useScopedSelection(scope: string, shown: readonly string[]) {
  const [selection, setSelection] = useState<ScopedSelection>(() => ({
    scope,
    shown: [...shown],
    chosen: [],
    owner: {},
  }));
  if (selection.scope !== scope) {
    setSelection({ scope, shown: [...shown], chosen: [], owner: {} });
  } else if (
    selection.shown.length !== shown.length ||
    selection.shown.some((id, index) => id !== shown[index])
  ) {
    setSelection({ ...selection, shown: [...shown] });
  }
  const owner = selection.owner;
  const chosen = selection.scope === scope ? selection.chosen : [];
  const clearChosen = useCallback(
    () =>
      setSelection((prev) =>
        prev.scope === scope && prev.owner === owner
          ? { ...prev, chosen: [], anchor: undefined }
          : prev,
      ),
    [scope, owner],
  );
  const toggle = useCallback(
    (root: string, extend = false, checked?: boolean) =>
      setSelection((prev) => {
        if (prev.scope !== scope || prev.owner !== owner) return prev;
        const next = toggleRow(prev, prev.shown, root, extend, checked);
        return next === prev ? prev : { ...prev, ...next };
      }),
    [scope, owner],
  );
  const toggleAll = useCallback(
    (checked?: boolean) =>
      setSelection((prev) => {
        if (prev.scope !== scope || prev.owner !== owner) return prev;
        const next = toggleShown(prev, prev.shown, checked);
        return next === prev ? prev : { ...prev, ...next, anchor: undefined };
      }),
    [scope, owner],
  );
  const ticks = new Set(chosen);
  const allSelected = shown.length > 0 && shown.every((id) => ticks.has(id));
  const mixed = !allSelected && shown.some((id) => ticks.has(id));
  const canClear =
    chosen.length > 0 || (selection.scope === scope && selection.anchor !== undefined);
  return { chosen, clearChosen, toggle, toggleAll, allSelected, mixed, canClear };
}
