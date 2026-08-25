/**
 * All page interactivity, as a framework-agnostic module that attaches to
 * already-rendered DOM by selector.
 *
 * Deliberately not React: the same behaviour has to run inside the dev app and
 * inside the server-rendered single-file export. Two implementations would drift.
 */
/** The listener registrar `attach` hands to the behaviours it delegates to. */
type On = (el: EventTarget, type: string, fn: (ev: Event) => void, opts?: AddEventListenerOptions) => void;

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

  /* ---------- in-page anchors scroll, not teleport ----------
   * Every internal cross-reference (reply spine, unspooled-from, xref, or a
   * permalink link) is a bare href="#id", and CSS sets
   * `scroll-behavior:smooth` on the root — so the browser's OWN fragment
   * navigation already smooth-scrolls to the target, updates the URL hash and
   * fires :target. No JS needed here; intercepting the click (as a earlier
   * version did to force `behavior:"smooth"`) is what the CSS makes redundant.
   */

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
   * Panel width is its content (fit to lane count, tallies, legend), not a fixed
   * column — but the toolbar, refresh verdict and reserved content column all
   * offset from it. Feed the measured rendered width back into --panel, and
   * re-measure when the panel is toggled or the window resizes.
   *
   * Reads the live offsetWidth, so when the panel is hidden (mapoff) no column
   * is reserved either.
   */
  const syncPanel = () => {
    if (!mini) return;
    if (body.classList.contains("mapoff")) body.style.removeProperty("--panel");
    else body.style.setProperty("--panel", `${Math.round(mini.offsetWidth)}px`);
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
    const apply = (hid: boolean) => {
      body.classList.toggle(cls, hid);
      btn.setAttribute("aria-pressed", hid ? "false" : "true");
      try { localStorage.setItem(key, hid ? "1" : "0"); } catch { /* private mode */ }
      syncPanel();
    };
    let stored: string | null = null;
    try { stored = localStorage.getItem(key); } catch { /* ignore */ }
    apply(stored === "1");
    on(btn, "click", () => apply(!body.classList.contains(cls)));
  }
  if (mini) { on(window, "resize", syncPanel); syncPanel(); }

  // A body is the sender's markup, so its emphasis and alignment are theirs, not
  // the page's. "plain" neutralises what is left of that presentation without
  // touching structure: a table stays a table because its columns carry the
  // meaning, and a list stays a list. Off by default — the sender's formatting is
  // usually what they meant.
  const plainCtl = toggle("plaintog", "plain", "cm-plain", false);
  if (plainCtl) on(plainCtl.btn, "click", () => plainCtl.set(!plainCtl.isOn()));

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

  /* ---------- enlarging a preview ---------- */
  cleanups.push(attachPopover(doc, on));

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
      // The minimap's nodes are SVG rectangles, not links, so they still need JS
      // to say *which* message to show — but CSS scroll-behavior:smooth makes
      // the resulting scroll glide, so no behavior:"smooth" is needed here.
      on(hit, "click", () => {
        el?.scrollIntoView({ block: "center" });
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

/**
 * Click-to-enlarge for attachment thumbnails, and for pictures the sender put
 * in the body.
 *
 * Not a `<details>`: a disclosure reveals content in place and leaves the
 * document readable around it, which is right for the panels and the signature
 * folds. An enlarged screenshot wants the opposite — it covers the transcript,
 * takes the keyboard, and is dismissed rather than left open. Dressing that up
 * as a disclosure would give it a summary marker that behaves like nothing else
 * on the page.
 *
 * Nor a native `<dialog>`, which would give modality and the focus trap for
 * free. Its `showModal` is absent from the DOM implementation the tests run in,
 * so choosing it would mean the trap — the part most likely to be got wrong —
 * could never be asserted. The cost of doing it by hand is one Tab handler.
 */
function attachPopover(doc: Document, on: On): () => void {
  // Every trigger is an element that already navigates somewhere on its own, so
  // the popover is strictly additive: with no script the click opens the file at
  // its source, and body pictures stay the plain images the sender sent.
  const triggers = [
    ...doc.querySelectorAll<HTMLElement>(".att[data-pop]"),
    ...doc.querySelectorAll<HTMLImageElement>(".bd img"),
  ];
  if (!triggers.length) return () => {};

  let host: HTMLElement | null = null;
  let shot: HTMLImageElement;
  let cap: HTMLElement;
  let closeBtn: HTMLButtonElement;
  let opener: HTMLElement | null = null;

  /** Built on first use, so a page nobody enlarges anything on carries no extra DOM. */
  const build = () => {
    if (host) return;
    host = doc.createElement("div");
    host.className = "pop";
    host.setAttribute("role", "dialog");
    host.setAttribute("aria-modal", "true");
    host.setAttribute("aria-label", "Enlarged image");
    host.hidden = true;
    host.innerHTML =
      '<div class="popbox"><img class="popimg" alt=""><div class="popbar">' +
      '<span class="popcap"></span><button type="button" class="popx">Close</button>' +
      "</div></div>";
    shot = host.querySelector<HTMLImageElement>(".popimg")!;
    cap = host.querySelector<HTMLElement>(".popcap")!;
    closeBtn = host.querySelector<HTMLButtonElement>(".popx")!;
    doc.body.appendChild(host);

    on(closeBtn, "click", close);
    // The backdrop is the host itself; a click that lands on the picture or the
    // bar must not dismiss, or dragging to select the caption closes the popover.
    on(host, "click", (ev: Event) => { if (ev.target === host) close(); });
    on(host, "keydown", (ev: Event) => {
      const k = ev as KeyboardEvent;
      if (k.key === "Escape") { k.preventDefault(); close(); return; }
      // Close is the only focusable thing inside, so the trap is Tab staying put
      // rather than a cycle through a list. Written as a wrap anyway: it stays
      // correct if the popover ever gains a second control.
      if (k.key !== "Tab") return;
      const stops = [...host!.querySelectorAll<HTMLElement>("button, [href]")];
      if (!stops.length) return;
      const edge = k.shiftKey ? stops[0]! : stops[stops.length - 1]!;
      if (doc.activeElement === edge || !host!.contains(doc.activeElement)) {
        k.preventDefault();
        (k.shiftKey ? stops[stops.length - 1]! : stops[0]!).focus();
      }
    });
  };

  function close() {
    if (!host || host.hidden) return;
    host.hidden = true;
    doc.body.classList.remove("popped");
    doc.querySelector(".wrap")?.removeAttribute("inert");
    // Returning the keyboard where it came from: a reader who enlarged a picture
    // mid-transcript must not be dropped back at the top of the document.
    opener?.focus();
    opener = null;
  }

  const open = (src: string, caption: string, from: HTMLElement) => {
    build();
    shot.src = src;
    cap.textContent = caption;
    host!.hidden = false;
    // The overlay covers the viewport, so a pointer cannot reach the transcript
    // anyway; inert is what says the same thing to a screen reader and to the
    // tab order, which the Tab handler alone could only enforce for the keyboard.
    doc.querySelector(".wrap")?.setAttribute("inert", "");
    doc.body.classList.add("popped");
    opener = from;
    closeBtn.focus();
  };

  /** Wire one picture up, once it is known to be worth enlarging. */
  const arm = (t: HTMLElement, img: HTMLImageElement, isChip: boolean) => {
    const label = isChip ? t.dataset.pop! : img.getAttribute("alt") || "";
    // The keyboard has to reach whatever already takes focus. A chip is the link
    // itself; a body picture may be wrapped in one, and the wrapper is the tab
    // stop, so a listener on the picture would never see the Enter key.
    const trig: HTMLElement = isChip ? t : (t.closest("a") ?? t);
    if (trig === t && !isChip && !t.hasAttribute("tabindex")) t.tabIndex = 0;
    trig.setAttribute("aria-haspopup", "dialog");

    const show = () => open(img.currentSrc || img.src, label, trig);
    on(trig, "click", (ev) => {
      const m = ev as MouseEvent;
      // A modified click is the reader asking for a new tab or a download, and
      // the link underneath is the right answer to that. Leave it alone.
      if (m.metaKey || m.ctrlKey || m.shiftKey || m.altKey || m.button !== 0) return;
      ev.preventDefault();
      show();
    });
    // An anchor already fires a click on Enter; a bare picture does not, and
    // Space would otherwise scroll the page out from under it.
    if (trig.tagName !== "A") {
      on(trig, "keydown", (ev) => {
        const k = ev as KeyboardEvent;
        if (k.key !== "Enter" && k.key !== " ") return;
        k.preventDefault();
        show();
      });
    }
  };

  for (const t of triggers) {
    const isChip = t.classList.contains("att");
    const img = isChip ? t.querySelector<HTMLImageElement>(".athumb") : (t as HTMLImageElement);
    if (!img) continue;
    // A chip's thumbnail was already judged worth showing where the bytes were,
    // so it is not judged again.
    if (isChip) {
      arm(t, img, true);
      continue;
    }
    // A body picture has had no such filter: it is whatever the sender's markup
    // contained, which on a real mail page is mostly letterhead, wordmarks and
    // tracking pixels — 21 of 29 on one. Nothing is armed until the picture is
    // known to be worth it, because arming adds a tab stop, and a tab stop that
    // enlarges a 120×22 wordmark is worse than none.
    if (!img.getAttribute("src")) continue;
    switch (worthEnlarging(img)) {
      case "yes":
        arm(t, img, false);
        break;
      case "unknown":
        // No declared size and not yet loaded, so nothing can be measured. Ask
        // again when the bytes arrive, which is the first moment the picture's
        // own dimensions exist.
        on(img, "load", () => { if (worthEnlarging(img) === "yes") arm(t, img, false); },
          { once: true });
        break;
    }
  }

  return () => { host?.remove(); host = null; };
}

/**
 * The floor separating a picture worth enlarging from decoration, in pixels on
 * both edges. Deliberately the same number the spec builder applies to decoded
 * attachment bytes (minPreviewEdge in preview.go): one rule for "this is a
 * picture and not furniture", so the page never offers to enlarge something the
 * builder would have refused to embed.
 */
const MIN_ENLARGE_EDGE = 100;

/**
 * Whether a body picture is worth a popover, or whether that cannot be told yet.
 *
 * Two things disqualify one: being small enough to be furniture, and already
 * being on screen at its full size, where enlarging shows nothing new. Both are
 * measured where the measurement exists — a loaded picture knows its natural
 * size, an unloaded one only has the width and height the sender declared, and a
 * picture with neither cannot be judged at all until it loads.
 */
function worthEnlarging(img: HTMLImageElement): "yes" | "no" | "unknown" {
  const box = img.clientWidth || Number(img.getAttribute("width")) || 0;
  const boxH = img.clientHeight || Number(img.getAttribute("height")) || 0;
  const nat = img.naturalWidth;
  if (box && boxH && (box < MIN_ENLARGE_EDGE || boxH < MIN_ENLARGE_EDGE)) return "no";
  if (nat) {
    if (nat < MIN_ENLARGE_EDGE || img.naturalHeight < MIN_ENLARGE_EDGE) return "no";
    // Shown at full size already. Compared against the box rather than the
    // viewport: a picture the sender sized down is worth opening, one they did
    // not is already as large as it gets.
    if (box && nat <= box) return "no";
    return "yes";
  }
  return box && boxH ? "yes" : "unknown";
}
