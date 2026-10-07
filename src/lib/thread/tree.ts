export interface Knot<E> {
  entry: E;
  replies: Knot<E>[];
}

/**
 * A reply forest. Entries whose parent is absent open their own tree, and entries
 * caught in a cycle are appended as roots at the end rather than recursing forever.
 */
export function tree<E>(
  entries: E[],
  key: (e: E) => string,
  parent: (e: E) => string | undefined,
): Knot<E>[] {
  const known = new Set(entries.map(key));
  const children = new Map<string, E[]>();
  const roots: E[] = [];
  for (const e of entries) {
    const p = parent(e);
    if (p === undefined || p === "" || p === key(e) || !known.has(p)) roots.push(e);
    else {
      const siblings = children.get(p);
      if (siblings) siblings.push(e);
      else children.set(p, [e]);
    }
  }

  const out: Knot<E>[] = [];
  const drawn = new Set<string>();
  const walk = (e: E, into: Knot<E>[]) => {
    const k = key(e);
    if (drawn.has(k)) return;
    drawn.add(k);
    const knot: Knot<E> = { entry: e, replies: [] };
    into.push(knot);
    for (const reply of children.get(k) ?? []) walk(reply, knot.replies);
  };
  for (const root of roots) walk(root, out);
  for (const e of entries) if (!drawn.has(key(e))) walk(e, out);

  return out;
}
