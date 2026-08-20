/**
 * All page interactivity, as a framework-agnostic module that attaches to
 * already-rendered DOM by selector.
 *
 * Deliberately not React: the same behaviour has to run inside the dev app and
 * inside the server-rendered single-file export. Two implementations would drift.
 */
export function attach(doc: Document = document): () => void {
  const cleanups: Array<() => void> = [];
  const on = <K extends keyof HTMLElementEventMap>(
    el: EventTarget,
    type: K | string,
    fn: (ev: Event) => void,
    opts?: AddEventListenerOptions,
  ) => {
    el.addEventListener(type, fn as EventListener, opts);
    cleanups.push(() => el.removeEventListener(type, fn as EventListener));
  };

  const body = doc.body;
  const mini = doc.getElementById("mini");
  const entries = [...doc.querySelectorAll<HTMLElement>(".msg[id], .sys[id]")];

  /* ---------- toggles ---------- */
  const toggle = (id: string, cls: string, key: string, defaultOn: boolean) => {
    const btn = doc.getElementById(id) as HTMLButtonElement | null;
    if (!btn) return null;
    const set = (isOn: boolean) => {
      body.classList.toggle(cls, isOn);
      btn.setAttribute("aria-pressed", isOn ? "true" : "false");
      try { localStorage.setItem(key, isOn ? "1" : "0"); } catch { /* private mode */ }
    };
    let stored: string | null = null;
    try { stored = localStorage.getItem(key); } catch { /* ignore */ }
    set(stored === null ? defaultOn : stored === "1");
    return { btn, set, isOn: () => body.classList.contains(cls) };
  };

  /**
   * Panels are shown by default, so the body class marks the HIDDEN state and the
   * button's pressed state is its inverse. One table rather than three near-copies.
   */
  const HIDEABLE: Array<[btn: string, cls: string, key: string]> = [
    ["maptog", "mapoff", "cm-tree"],
  ];
  for (const [id, cls, key] of HIDEABLE) {
    const btn = doc.getElementById(id) as HTMLButtonElement | null;
    if (!btn) continue;
    const apply = (hidden: boolean) => {
      body.classList.toggle(cls, hidden);
      btn.setAttribute("aria-pressed", hidden ? "false" : "true");
      try { localStorage.setItem(key, hidden ? "1" : "0"); } catch { /* private mode */ }
    };
    let stored: string | null = null;
    try { stored = localStorage.getItem(key); } catch { /* ignore */ }
    apply(stored === "1");
    on(btn, "click", () => apply(!body.classList.contains(cls)));
  }

  const viewCtl = toggle("viewtog", "chains", "cm-view", false);
  if (viewCtl) {
    on(viewCtl.btn, "click", () => {
      const next = !viewCtl.isOn();
      const keep = hovId ?? spyId;
      withTransition(doc, () => {
        viewCtl.set(next);
        // re-anchor inside the callback so the transition animates to the final
        // scrolled position instead of landing and then jumping
        if (keep) doc.getElementById(keep)?.scrollIntoView({ block: "center" });
      });
    });
  }

  /* ---------- highlight: one state, two drivers ---------- */
  const nodes = mini ? [...mini.querySelectorAll<SVGGElement>(".nd")] : [];
  const links = mini ? [...mini.querySelectorAll<SVGPathElement>(".lk")] : [];
  const parentOf = new Map<string, string | null>(
    nodes.map((n) => [n.dataset.id!, n.dataset.p || null]),
  );
  const nodeById = new Map(nodes.map((n) => [n.dataset.id!, n]));

  let spyId: string | null = null;
  let hovId: string | null = null;
  let hovChain: string | null = null;

  /** Chain root of a node, by walking the reply graph the minimap already carries. */
  const rootCache = new Map<string, string>();
  const rootOf = (id: string): string => {
    const hit = rootCache.get(id);
    if (hit) return hit;
    let cur = id;
    const seen = new Set<string>();
    for (;;) {
      const p = parentOf.get(cur);
      if (!p || seen.has(cur)) break;
      seen.add(cur);
      cur = p;
    }
    rootCache.set(id, cur);
    return cur;
  };

  const lightChain = (root: string) => {
    if (!mini) return;
    const members = new Set(nodes.filter((n) => rootOf(n.dataset.id!) === root)
      .map((n) => n.dataset.id!));
    for (const n of nodes) {
      n.classList.remove("cur", "hov", "anc");
      n.classList.toggle("chn", members.has(n.dataset.id!));
    }
    for (const l of links) {
      l.classList.remove("anc");
      l.classList.toggle("chn", members.has(l.dataset.c!));
    }
    mini.classList.add("spy");
  };

  const clearChain = () => {
    for (const n of nodes) n.classList.remove("chn");
    for (const l of links) l.classList.remove("chn");
  };

  const light = (id: string, hover: boolean) => {
    if (!mini) return;
    const chain = new Set<string>();
    for (let c: string | null | undefined = id; c; c = parentOf.get(c)) chain.add(c);
    for (const n of nodes) {
      const me = n.dataset.id === id;
      n.classList.toggle("cur", me);
      n.classList.toggle("hov", me && hover);
      n.classList.toggle("anc", chain.has(n.dataset.id!) && !me);
    }
    for (const l of links) l.classList.toggle("anc", chain.has(l.dataset.c!));
    mini.classList.add("spy");
    const el = nodeById.get(id);
    const scroller = mini.querySelector<HTMLElement>(".mbody");
    if (el && scroller) {
      const r = el.getBoundingClientRect();
      const b = scroller.getBoundingClientRect();
      if (r.top < b.top + 24 || r.bottom > b.bottom - 24) {
        scroller.scrollTop += r.top - b.top - scroller.clientHeight / 2;
      }
    }
  };
  // hovering a chain in the sources panel outranks both the pointer and the
  // scroll position, since it is the most explicit thing the reader asked for
  const refresh = () => {
    if (hovChain) { lightChain(hovChain); return; }
    clearChain();
    const id = hovId ?? spyId;
    if (id) light(id, hovId !== null);
  };

  /* ---------- the minimap's full-width row strips own the pointer ---------- */
  if (mini) {
    for (const hit of mini.querySelectorAll<SVGRectElement>(".hit")) {
      const id = hit.dataset.id!;
      const el = doc.getElementById(id);
      on(hit, "click", () => {
        el?.scrollIntoView({ block: "center", behavior: "smooth" });
        history.replaceState(null, "", `#${id}`);
      });
      on(hit, "mouseenter", () => { el?.classList.add("mhov"); hovId = id; refresh(); });
      on(hit, "mouseleave", () => {
        el?.classList.remove("mhov");
        if (hovId === id) { hovId = null; refresh(); }
      });
    }
  }

  /* ---------- hovering a chain row in the sources panel ---------- */
  for (const row of doc.querySelectorAll<HTMLElement>("[data-chain]")) {
    const root = row.dataset.chain!;
    on(row, "mouseenter", () => { hovChain = root; refresh(); });
    on(row, "mouseleave", () => {
      if (hovChain === root) { hovChain = null; refresh(); }
    });
  }

  /* ---------- scroll-spy + hovering a message ---------- */
  const visible = new Map<string, number>();
  const io = new IntersectionObserver(
    (records) => {
      for (const r of records) {
        if (r.isIntersecting) visible.set(r.target.id, r.boundingClientRect.top);
        else visible.delete(r.target.id);
      }
      let best: string | null = null;
      let top = Infinity;
      for (const [id, y] of visible) if (y < top) { top = y; best = id; }
      if (best) { spyId = best; refresh(); }
    },
    { rootMargin: "-8% 0px -55% 0px" },
  );
  cleanups.push(() => io.disconnect());
  for (const el of entries) {
    io.observe(el);
    on(el, "mouseenter", () => { if (nodeById.has(el.id)) { hovId = el.id; refresh(); } });
    on(el, "mouseleave", () => { if (hovId === el.id) { hovId = null; refresh(); } });
  }

  return () => { for (const c of cleanups) c(); };
}

/**
 * Name only the entries on screen before transitioning: dozens of transition
 * groups is needless work, and the ones off screen cannot be perceived. Names are
 * cleared afterwards so they never affect a later transition.
 */
function withTransition(doc: Document, apply: () => void) {
  type WithVT = Document & { startViewTransition?: (cb: () => void) => { finished: Promise<void> } };
  const d = doc as WithVT;
  const reduce = window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;
  if (!d.startViewTransition || reduce) { apply(); return; }

  const named: HTMLElement[] = [];
  const vh = window.innerHeight;
  for (const el of doc.querySelectorAll<HTMLElement>(".msg[id], .sys[id]")) {
    if (named.length >= 24) break;
    const r = el.getBoundingClientRect();
    if (r.bottom > -vh * 0.25 && r.top < vh * 1.25) {
      el.style.viewTransitionName = el.id;
      named.push(el);
    }
  }
  const clear = () => { for (const el of named) el.style.viewTransitionName = ""; };
  d.startViewTransition(apply).finished.then(clear, clear);
}
