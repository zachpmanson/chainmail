import { useCallback, useState } from "react";

/** Clear ticks before rendering a new scope, without remounting its reader or composer. */
export default function useScopedSelection(scope: string) {
  const [selection, setSelection] = useState<{ scope: string; chosen: string[] }>({
    scope,
    chosen: [],
  });
  if (selection.scope !== scope) setSelection({ scope, chosen: [] });
  const chosen = selection.scope === scope ? selection.chosen : [];
  const clearChosen = useCallback(
    () => setSelection((prev) => (prev.scope === scope ? { scope, chosen: [] } : prev)),
    [scope],
  );
  const toggle = useCallback(
    (root: string) =>
      setSelection((prev) =>
        prev.scope !== scope
          ? prev
          : {
              scope,
              chosen: prev.chosen.includes(root)
                ? prev.chosen.filter((id) => id !== root)
                : [...prev.chosen, root],
            },
      ),
    [scope],
  );
  return { chosen, clearChosen, toggle };
}
