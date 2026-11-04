export type RowSelection = { chosen: string[]; anchor?: string };

export function toggleRow(
  selection: RowSelection,
  shown: readonly string[],
  root: string,
  extend = false,
  checked?: boolean,
): RowSelection {
  const end = shown.indexOf(root);
  if (end < 0) return selection;
  const start = extend && selection.anchor ? shown.indexOf(selection.anchor) : -1;
  const range = start < 0 ? [root] : shown.slice(Math.min(start, end), Math.max(start, end) + 1);
  const chosen = new Set(selection.chosen);
  const select = checked ?? !chosen.has(root);
  for (const id of range) {
    if (select) chosen.add(id);
    else chosen.delete(id);
  }
  return { chosen: [...chosen], anchor: start < 0 ? root : selection.anchor };
}

export function toggleShown(
  selection: RowSelection,
  shown: readonly string[],
  checked?: boolean,
): RowSelection {
  if (!shown.length) return selection;
  const chosen = new Set(selection.chosen);
  const select = checked ?? !shown.every((id) => chosen.has(id));
  for (const id of shown) {
    if (select) chosen.add(id);
    else chosen.delete(id);
  }
  return { chosen: [...chosen] };
}
