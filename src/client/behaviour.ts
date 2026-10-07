import { readTable, type Table } from "../lib/message/tables";
import { withTransition } from "../lib/thread/viewTransition";

/** Not React: the same behaviour runs in the dev app and the server-rendered single-file export. */
/** The listener registrar `attach` hands to the behaviours it delegates to. */
type On = (
  el: EventTarget,
  type: string,
  fn: (ev: Event) => void,
  opts?: AddEventListenerOptions,
) => void;

// Pointer slack around a 2px reply line; less than the indent gutter beside it.
const LINE_REACH = 5;

/** Arithmetic, not `:hover`: a nested line's hover hits every ancestor, and `:has()` can't nest. */
export function lineAt(doc: Document, x: number, y: number): HTMLElement | null {
  let found: HTMLElement | null = null;
  let deepest = -Infinity;
  for (const line of doc.querySelectorAll<HTMLElement>(".ibread .stream .replies")) {
    const r = line.getBoundingClientRect();
    if (x < r.left - LINE_REACH || x > r.left + LINE_REACH) continue;
    if (y < r.top || y > r.bottom) continue;
    if (r.left > deepest) {
      deepest = r.left;
      found = line;
    }
  }
  return found;
}

// A card's hit box includes its margin-bottom, so the mark doesn't blink out between messages.
const MARGIN_REACH = 9;

export function messageAt(doc: Document, x: number, y: number): HTMLElement | null {
  let found: HTMLElement | null = null;
  let deepest = -Infinity;
  for (const card of doc.querySelectorAll<HTMLElement>(".ibread .stream .msg")) {
    const r = card.getBoundingClientRect();
    if (x < r.left || x > r.right) continue;
    if (y < r.top || y > r.bottom + MARGIN_REACH) continue;
    if (r.left > deepest) {
      deepest = r.left;
      found = card;
    }
  }
  return found;
}

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
      try {
        localStorage.setItem(key, isOn ? "1" : "0");
      } catch {
        /* private mode */
      }
    };
    let stored: string | null = null;
    try {
      stored = localStorage.getItem(key);
    } catch {
      /* ignore */
    }
    set(stored === null ? defaultOn : stored === "1");
    return { btn, set, isOn: () => body.classList.contains(cls) };
  };

  // Feed the panel's rendered width into --panel, which the toolbar and content column offset from.
  const syncPanel = () => {
    if (!mini) return;
    if (body.classList.contains("mapoff") || body.classList.contains("tree-h"))
      body.style.removeProperty("--panel");
    else body.style.setProperty("--panel", `${Math.round(mini.offsetWidth)}px`);
  };

  /** The tree button's three states, in click order. */
  const TREE_MODES = ["v", "h", "off"] as const;
  type TreeMode = (typeof TREE_MODES)[number];
  /** The button names each state, so the mode is visible without the panel. */
  const treeLabel = (m: TreeMode) =>
    m === "v"
      ? "Reply tree: vertical — click for horizontal"
      : m === "h"
        ? "Reply tree: horizontal — click to hide"
        : "Reply tree: off — click for vertical";

  const maptog = doc.getElementById("maptog") as HTMLButtonElement | null;
  if (maptog) {
    let mode: TreeMode = "v";
    const fromStored = (s: string | null): TreeMode => {
      if (s === "v" || s === "h") return s;
      // the old key stored "1"/"0" (hidden/shown); migrate "1" to off
      if (s === "off" || s === "1") return "off";
      return "v";
    };
    const apply = (m: TreeMode) => {
      mode = m;
      body.classList.toggle("mapoff", m === "off");
      body.classList.toggle("tree-h", m === "h");
      maptog.setAttribute("aria-pressed", m === "off" ? "false" : "true");
      maptog.setAttribute("aria-label", treeLabel(m));
      try {
        localStorage.setItem("cm-tree", m);
      } catch {
        /* private mode */
      }
      syncPanel();
    };
    let stored: string | null = null;
    try {
      stored = localStorage.getItem("cm-tree");
    } catch {
      /* ignore */
    }
    apply(fromStored(stored));
    on(maptog, "click", () => {
      const i = TREE_MODES.indexOf(mode);
      apply(TREE_MODES[(i + 1) % TREE_MODES.length]!);
    });
  }
  if (mini) {
    on(window, "resize", syncPanel);
    syncPanel();
  }

  const plainCtl = toggle("plaintog", "plain", "cm-plain", false);
  if (plainCtl) on(plainCtl.btn, "click", () => plainCtl.set(!plainCtl.isOn()));

  const viewCtl = toggle("viewtog", "chains", "cm-view", false);
  if (viewCtl) {
    on(viewCtl.btn, "click", () => {
      const next = !viewCtl.isOn();
      const keep = hovId ?? spyId;
      withTransition(doc, () => {
        viewCtl.set(next);
        // Re-anchor inside the callback so the transition animates to the final scroll position.
        if (keep) doc.getElementById(keep)?.scrollIntoView({ block: "center" });
      });
    });
  }

  /* ---------- enlarging a preview ---------- */
  cleanups.push(attachPopover(doc, on));

  /* Replays a download press once the re-render puts bytes behind the chip. The flag lives on
     the element because the render replaces every attachment, and an attribute survives that. */
  for (const chip of doc.querySelectorAll<HTMLElement>("[data-attachment][data-download]")) {
    if (!chip.dataset.get) continue;
    chip.removeAttribute("data-download");
    chip.click();
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
    const members = new Set(
      nodes.filter((n) => rootOf(n.dataset.id!) === root).map((n) => n.dataset.id!),
    );
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

  /* Centre once per entry, and not just after the reader scrolls the panel, or it snaps back. */
  let centred: string | null = null;
  let readerTouchedAt = 0;
  const READER_GRACE_MS = 2500;
  // Our last write per scroller, so the scroll event it fires isn't mistaken for the reader's.
  const ours = new Map<HTMLElement, [number, number]>();
  const readerTookOver = (id: string) =>
    id !== centred && Date.now() - readerTouchedAt > READER_GRACE_MS;
  if (mini) {
    for (const scroller of mini.querySelectorAll<HTMLElement>(".mbody")) {
      const touched = () => {
        const [top, left] = ours.get(scroller) ?? [NaN, NaN];
        if (Math.abs(scroller.scrollTop - top) > 1 || Math.abs(scroller.scrollLeft - left) > 1)
          readerTouchedAt = Date.now();
      };
      for (const ev of ["wheel", "touchmove", "keydown", "scroll"] as const)
        on(scroller, ev, touched);
    }
  }

  /** Move a scroller along time's axis, remembering the offset as ours. */
  const nudge = (scroller: HTMLElement, horizontal: boolean, by: number) => {
    if (horizontal) scroller.scrollLeft += by;
    else scroller.scrollTop += by;
    ours.set(scroller, [scroller.scrollTop, scroller.scrollLeft]);
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
    const scroller = mini.querySelector<HTMLElement>(
      body.classList.contains("tree-h") ? ".hrow .mbody" : ".vrow .mbody",
    );
    if (el && scroller && readerTookOver(id)) {
      const r = el.getBoundingClientRect();
      const b = scroller.getBoundingClientRect();
      const horizontal = body.classList.contains("tree-h");
      if (horizontal) {
        if (r.left < b.left + 24 || r.right > b.right - 24) {
          centred = id;
          nudge(scroller, true, r.left - b.left - scroller.clientWidth / 2);
        }
      } else if (r.top < b.top + 24 || r.bottom > b.bottom - 24) {
        centred = id;
        nudge(scroller, false, r.top - b.top - scroller.clientHeight / 2);
      }
    }
  };
  const refresh = () => {
    if (hovChain) {
      lightChain(hovChain);
      return;
    }
    clearChain();
    const id = hovId ?? spyId;
    if (id) light(id, hovId !== null);
  };

  /* ---------- the minimap's full-width row strips own the pointer ---------- */
  if (mini) {
    for (const hit of mini.querySelectorAll<SVGRectElement>("[data-hit]")) {
      const id = hit.dataset.id!;
      const el = doc.getElementById(id);
      on(hit, "click", () => {
        el?.scrollIntoView({ block: "center" });
        history.replaceState(null, "", `#${id}`);
      });
      on(hit, "mouseenter", () => {
        el?.classList.add("mhov");
        hovId = id;
        refresh();
      });
      on(hit, "mouseleave", () => {
        el?.classList.remove("mhov");
        if (hovId === id) {
          hovId = null;
          refresh();
        }
      });
    }
  }

  /* Lights every `.replies` line from the pointed message up to the root, walked in the DOM so it
     works in built pages. Only a change of path touches the DOM; re-adding classes restarts fades. */
  const stream = doc.querySelector<HTMLElement>(".ibread .stream");
  if (stream) {
    let lit: HTMLElement[] = [];
    let litFor: HTMLElement | null = null;
    const clear = () => {
      for (const el of lit) el.classList.remove("rhov");
      lit = [];
      litFor = null;
    };
    const light = (msg: HTMLElement | null) => {
      if (msg === litFor) return;
      clear();
      if (!msg) return;
      litFor = msg;
      for (let node = msg.parentElement; node && node !== stream; node = node.parentElement) {
        if (!node.classList.contains("replies")) continue;
        node.classList.add("rhov");
        lit.push(node);
      }
    };
    on(stream, "mousemove", (ev) => {
      const e = ev as MouseEvent;
      const under = lineAt(doc, e.clientX, e.clientY);
      const msg = under
        ? (under.previousElementSibling as HTMLElement | null)
        : messageAt(doc, e.clientX, e.clientY);
      light(msg?.classList.contains("msg") ? msg : null);
    });
    on(stream, "mouseleave", clear);
  }

  /* Reply links ring their target message (`.mhov`). Delegated from the document because React
     redraws these links without re-attaching this module; `ringed` lets detach clear stale rings. */
  const ringed = new Set<HTMLElement>();
  const ring = (el: HTMLElement | null, lit: boolean) => {
    if (!el) return;
    el.classList.toggle("mhov", lit);
    if (lit) ringed.add(el);
    else ringed.delete(el);
  };
  const claimAt = (el: EventTarget | null): { el: Element; id: string } | null => {
    if (!(el instanceof Element)) return null;
    const link = el.closest<HTMLAnchorElement>("a.par[href^='#']");
    return link ? { el: link, id: link.getAttribute("href")!.slice(1) } : null;
  };
  for (const type of ["mouseover", "mouseout"] as const) {
    on(doc, type, (ev) => {
      const claim = claimAt(ev.target);
      if (!claim) return;
      // Ignore moves between children of the same link.
      if (claimAt((ev as MouseEvent).relatedTarget)?.el === claim.el) return;
      ring(doc.getElementById(claim.id), type === "mouseover");
    });
  }
  cleanups.push(() => {
    for (const el of ringed) el.classList.remove("mhov");
    ringed.clear();
  });

  /* ---------- hovering a chain row in the sources panel ---------- */
  for (const row of doc.querySelectorAll<HTMLElement>("[data-chain]")) {
    const root = row.dataset.chain!;
    on(row, "mouseenter", () => {
      hovChain = root;
      refresh();
    });
    on(row, "mouseleave", () => {
      if (hovChain === root) {
        hovChain = null;
        refresh();
      }
    });
  }

  /* ---------- scroll-spy + hovering a message ---------- */
  const visible = new Map<string, number>();
  // Guarded: without IntersectionObserver, lose the highlight rather than throw in a React effect.
  const io =
    typeof IntersectionObserver === "function"
      ? new IntersectionObserver(
          (records) => {
            for (const r of records) {
              if (r.isIntersecting) visible.set(r.target.id, r.boundingClientRect.top);
              else visible.delete(r.target.id);
            }
            let best: string | null = null;
            let top = Infinity;
            for (const [id, y] of visible)
              if (y < top) {
                top = y;
                best = id;
              }
            if (best) {
              spyId = best;
              refresh();
            }
          },
          { rootMargin: "-8% 0px -55% 0px" },
        )
      : null;
  if (io) cleanups.push(() => io.disconnect());
  for (const el of entries) {
    if (io) io.observe(el);
    on(el, "mouseenter", () => {
      if (nodeById.has(el.id)) {
        hovId = el.id;
        refresh();
      }
    });
    on(el, "mouseleave", () => {
      if (hovId === el.id) {
        hovId = null;
        refresh();
      }
    });
  }

  return () => {
    for (const c of cleanups) c();
  };
}

/** Click-to-enlarge window for attachments and body pictures. Not a native `<dialog>` because
 *  jsdom lacks `showModal`, so the hand-rolled focus trap stays testable. */
function attachPopover(doc: Document, on: On): () => void {
  // Strictly additive: without script, triggers still navigate to the file.
  const triggers = [
    ...doc.querySelectorAll<HTMLElement>("[data-attachment][data-pop]"),
    ...doc.querySelectorAll<HTMLImageElement>(".bd img"),
  ];
  if (!triggers.length) return () => {};

  let host: HTMLElement | null = null;
  let shot: HTMLImageElement;
  let text: HTMLElement;
  let grid: HTMLElement;
  let frame: HTMLIFrameElement;
  let cap: HTMLElement;
  let note: HTMLElement;
  let save: HTMLAnchorElement;
  let closeBtn: HTMLButtonElement;
  let opener: HTMLElement | null = null;
  // Each open bumps this; a late fetch for an earlier open is dropped.
  let opening = 0;
  let blobURL = "";

  /** Built on first use, so a page nobody enlarges anything on carries no extra DOM. */
  const build = () => {
    if (host) return;
    host = doc.createElement("div");
    host.className =
      "pop fixed inset-0 z-[55] flex items-center justify-center p-6 bg-bg/92 backdrop-blur-[3px]";
    host.setAttribute("role", "dialog");
    host.setAttribute("aria-modal", "true");
    host.setAttribute("aria-labelledby", "popcap");
    host.hidden = true;
    host.innerHTML =
      '<div class="flex max-h-full max-w-full flex-col gap-1.5"><img class="popimg h-auto w-auto max-h-[calc(100vh-6rem)] max-w-full rounded-md border border-line bg-card object-contain" alt="" hidden>' +
      '<pre class="poptext m-0 max-h-[calc(100vh-6rem)] w-[min(72rem,92vw)] overflow-auto whitespace-pre rounded-md border border-line bg-card p-4 text-left font-mono text-[.78rem] leading-[1.5] text-fg [tab-size:4]" hidden></pre>' +
      // Separate from the `pre` so the table doesn't inherit `white-space:pre`.
      '<div class="popgrid max-h-[calc(100vh-6rem)] w-[min(72rem,92vw)] overflow-auto rounded-md border border-line bg-card text-left" hidden></div>' +
      // Blob type comes from the served bytes, never the sender's claim; that makes framing safe.
      '<iframe class="popframe h-[calc(100vh-6rem)] w-[min(72rem,92vw)] rounded-md border border-line bg-card" title="" hidden></iframe>' +
      '<div class="flex items-center gap-3">' +
      '<span class="overflow-hidden text-ellipsis whitespace-nowrap font-mono text-[.74rem] font-semibold text-muted" id="popcap"></span><span data-popnote class="whitespace-nowrap text-[.72rem] text-muted"></span>' +
      '<a class="popget cursor-pointer rounded-md border border-line bg-card px-2 py-0.5 font-[inherit] text-[.72rem] text-muted no-underline hover:border-accent hover:text-fg focus-visible:border-accent" download hidden>save</a>' +
      '<button type="button" data-popclose class="ml-auto cursor-pointer rounded-md border border-line bg-card px-2 py-0.5 font-[inherit] text-[.72rem] text-fg hover:border-accent focus-visible:border-accent">Close</button>' +
      "</div></div>";
    shot = host.querySelector<HTMLImageElement>(".popimg")!;
    text = host.querySelector<HTMLElement>(".poptext")!;
    grid = host.querySelector<HTMLElement>(".popgrid")!;
    frame = host.querySelector<HTMLIFrameElement>(".popframe")!;
    cap = host.querySelector<HTMLElement>("#popcap")!;
    note = host.querySelector<HTMLElement>("[data-popnote]")!;
    save = host.querySelector<HTMLAnchorElement>(".popget")!;
    closeBtn = host.querySelector<HTMLButtonElement>("[data-popclose]")!;
    doc.body.appendChild(host);

    on(closeBtn, "click", close);
    // Only backdrop clicks dismiss, so drag-selecting the caption doesn't close it.
    on(host, "click", (ev: Event) => {
      if (ev.target === host) close();
    });
    on(host, "keydown", (ev: Event) => {
      const k = ev as KeyboardEvent;
      // A framed PDF swallows Escape and Tab, so Close and the backdrop are its way out.
      if (k.key === "Escape") {
        k.preventDefault();
        close();
        return;
      }
      // Skip `hidden` stops: they can't take focus, and the trap would land on nothing.
      if (k.key !== "Tab") return;
      const stops = [
        ...host!.querySelectorAll<HTMLElement>("button:not([hidden]), [href]:not([hidden])"),
      ];
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
    opening++;
    empty();
    host.hidden = true;
    doc.body.classList.remove("popped");
    doc.querySelector(".wrap")?.removeAttribute("inert");
    opener?.focus();
    opener = null;
  }

  /** Put the window back to empty: nothing shown, nothing held. */
  function empty() {
    if (!host) return;
    shot.hidden = true;
    text.hidden = true;
    text.textContent = "";
    grid.hidden = true;
    grid.textContent = "";
    frame.hidden = true;
    frame.removeAttribute("src");
    note.textContent = "";
    if (blobURL) {
      URL.revokeObjectURL(blobURL);
      blobURL = "";
    }
  }

  /**
   * Pictures load the full bytes; the embedded preview (capped at 640px) is the fallback when
   * they're pruned. PDFs become a blob because a frame won't render the served attachment URL.
   */
  const open = (
    from: HTMLElement,
    caption: string,
    preview: string,
    full: string,
    view: string,
  ) => {
    build();
    const mine = ++opening;
    empty();
    cap.textContent = caption;
    shot.alt = caption;
    frame.title = caption;
    // The chip's click is intercepted, so this is the only route to the original file.
    save.hidden = !full;
    if (full) save.href = full;
    if (view === "image") {
      shot.onerror = () => {
        shot.onerror = null;
        if (preview) shot.src = preview;
      };
      shot.src = full || preview;
      shot.hidden = false;
    } else if (full) {
      void bring(full, view, mine, caption);
    }
    host!.hidden = false;
    // inert hides the transcript from screen readers and the tab order too.
    doc.querySelector(".wrap")?.setAttribute("inert", "");
    doc.body.classList.add("popped");
    opener = from;
    closeBtn.focus();
  };

  /** Fetch the bytes and put them in the window, unless the reader has moved on. */
  const bring = async (url: string, view: string, mine: number, caption: string) => {
    let res: Response;
    try {
      res = await fetch(url);
      if (!res.ok) throw new Error(String(res.status));
      if (view === "text") {
        const body = await res.text();
        if (mine !== opening) return;
        const cut = body.length > TEXT_CAP;
        const shown = cut ? body.slice(0, TEXT_CAP) : body;
        // Sniffed from bytes and served type, so old saved pages get tables. `readTable` refuses prose.
        const table = readTable(shown, caption, res.headers.get("content-type") ?? "");
        if (table) {
          drawTable(table);
          note.textContent = tableNote(table, cut);
          grid.hidden = false;
          return;
        }
        text.textContent = shown;
        if (cut) note.textContent = "truncated — save it for the rest";
        text.hidden = false;
        return;
      }
      const blob = await res.blob();
      if (mine !== opening) return;
      blobURL = URL.createObjectURL(blob);
      frame.src = blobURL;
      frame.hidden = false;
    } catch {
      if (mine === opening) note.textContent = "could not be read here";
    }
  };

  /** Cells are sender bytes, so they go in via `textContent`, never markup. */
  const drawTable = (t: Table) => {
    const cell = (
      tag: "th" | "td",
      value: string,
      num: boolean,
      lastColumn: boolean,
      lastRow: boolean,
    ) => {
      const el = doc.createElement(tag);
      el.textContent = value;
      el.className = [
        "max-w-[26rem] border-r border-b border-line px-2 py-1 text-left align-top whitespace-pre-wrap [overflow-wrap:anywhere]",
        lastColumn && "border-r-0",
        lastRow && "border-b-0",
        tag === "th" && "sticky top-0 z-[1] bg-card font-[650] text-muted",
        tag === "td" && num && "text-right tabular-nums",
      ]
        .filter(Boolean)
        .join(" ");
      return el;
    };
    const table = doc.createElement("table");
    table.className = "border-collapse font-mono text-[.74rem]";
    const head = doc.createElement("thead");
    const hr = doc.createElement("tr");
    for (const [i, value] of t.header.entries())
      hr.appendChild(cell("th", value, false, i === t.header.length - 1, t.rows.length === 0));
    head.appendChild(hr);
    table.appendChild(head);
    const body = doc.createElement("tbody");
    for (const [ri, row] of t.rows.entries()) {
      const tr = doc.createElement("tr");
      for (const [i, value] of row.entries())
        tr.appendChild(
          cell("td", value, t.numeric[i] === true, i === row.length - 1, ri === t.rows.length - 1),
        );
      body.appendChild(tr);
    }
    table.appendChild(body);
    grid.textContent = "";
    grid.appendChild(table);
  };

  const tableNote = (t: Table, cut: boolean) => {
    const bits: string[] = [];
    if (t.rowCount > t.rows.length) bits.push(`first ${t.rows.length} of ${t.rowCount} rows`);
    if (t.colCount > t.header.length)
      bits.push(`first ${t.header.length} of ${t.colCount} columns`);
    if (cut) bits.push("truncated — save it for the rest");
    return bits.join(" · ");
  };

  /** `thumb` is null for a small stored picture, which the corpus never embedded. */
  const arm = (t: HTMLElement, thumb: HTMLImageElement | null, isChip: boolean) => {
    if (isChip && !thumb && !t.dataset.get) return;
    const label = isChip ? t.dataset.pop! : thumb?.getAttribute("alt") || "";
    // Listen on the focusable wrapper, or the picture never sees Enter.
    const trig: HTMLElement = isChip ? t : (t.closest("a") ?? t);
    if (trig === t && !isChip && !t.hasAttribute("tabindex")) t.tabIndex = 0;
    trig.setAttribute("aria-haspopup", "dialog");

    const view = isChip ? trig.dataset.view || "image" : "image";
    const show = () =>
      open(
        trig,
        label,
        thumb ? thumb.currentSrc || thumb.src : "",
        isChip ? (trig.dataset.get ?? "") : "",
        view,
      );
    on(trig, "click", (ev) => {
      const m = ev as MouseEvent;
      // Leave modified clicks (new tab, download) to the link.
      if (m.metaKey || m.ctrlKey || m.shiftKey || m.altKey || m.button !== 0) return;
      ev.preventDefault();
      show();
    });
    // A bare picture gets no click on Enter, and Space would scroll the page.
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
    const isChip = t.hasAttribute("data-attachment");
    if (isChip) {
      // Small pictures have no embedded preview, so the thumbnail is optional.
      arm(t, t.querySelector<HTMLImageElement>("img"), true);
      continue;
    }
    const img = t as HTMLImageElement;
    // Body pictures are mostly letterhead and tracking pixels; arming adds a tab stop, so filter.
    if (!img.getAttribute("src")) continue;
    switch (worthEnlarging(img)) {
      case "yes":
        arm(t, img, false);
        break;
      case "unknown":
        on(
          img,
          "load",
          () => {
            if (worthEnlarging(img) === "yes") arm(t, img, false);
          },
          { once: true },
        );
        break;
    }
  }

  return () => {
    host?.remove();
    host = null;
  };
}

// Matches minPreviewEdge in internal/spec/preview.go: don't enlarge what the builder won't embed.
const MIN_ENLARGE_EDGE = 100;

// In characters, since it limits what goes into the DOM.
const TEXT_CAP = 256 * 1024;

/** "no" if small enough to be furniture or already shown at full size; "unknown" until it loads. */
function worthEnlarging(img: HTMLImageElement): "yes" | "no" | "unknown" {
  const box = img.clientWidth || Number(img.getAttribute("width")) || 0;
  const boxH = img.clientHeight || Number(img.getAttribute("height")) || 0;
  const nat = img.naturalWidth;
  if (box && boxH && (box < MIN_ENLARGE_EDGE || boxH < MIN_ENLARGE_EDGE)) return "no";
  if (nat) {
    if (nat < MIN_ENLARGE_EDGE || img.naturalHeight < MIN_ENLARGE_EDGE) return "no";
    // Against the rendered box, not the viewport: a picture the sender sized down is worth opening.
    if (box && nat <= box) return "no";
    return "yes";
  }
  return box && boxH ? "yes" : "unknown";
}
