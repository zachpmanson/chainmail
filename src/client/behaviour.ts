import { readTable, type Table } from "../lib/tables";
import { withTransition } from "../lib/viewTransition";

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
    // the horizontal strip spans the viewport, so it reserves no column; only
    // the right-edge vertical panel does
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
      try { localStorage.setItem("cm-tree", m); } catch { /* private mode */ }
      syncPanel();
    };
    let stored: string | null = null;
    try { stored = localStorage.getItem("cm-tree"); } catch { /* ignore */ }
    apply(fromStored(stored));
    on(maptog, "click", () => {
      const i = TREE_MODES.indexOf(mode);
      apply(TREE_MODES[(i + 1) % TREE_MODES.length]!);
    });
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

  /* ---------- a download that was asked for before the bytes were here ----------
   *
   * A chip whose file is still in the mailbox is a download: the press asks the
   * host for it and marks itself (`data-download`, see Attachments), and the
   * renderer puts the bytes behind the chip a moment later — a rebuilt page, or a
   * re-read thread. The press is replayed here, at the first moment it can mean
   * what it meant.
   *
   * A click, rather than a second way to open things: whatever a chip does with
   * bytes in hand is what the reader asked for — the window for what this host can
   * show, the browser's own save for what it cannot — and one rule stays one rule.
   * The element is the only place that fact could have waited, because the render
   * that follows the pull replaces every attachment on the strip: state in the
   * renderer would have to survive that, and an attribute does by being there.
   *
   * Only a chip that has bytes behind it is replayed. One whose fetch failed, or
   * which the corpus then declined, keeps its mark and nothing happens: a later
   * pull that does bring the file is still the thing the reader asked for.
   */
  for (const chip of doc.querySelectorAll<HTMLElement>(".att[data-download]")) {
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

  /* The panel follows the page, but the reader can take it back. Centring on
     the spied entry on every light would undo a scroll the reader just made in
     the panel itself — they scroll down, the panel snaps back. So it is centred
     once per entry, and not at all for a moment after the reader scrolls the
     panel: a wheel or a drag in there means they are looking somewhere else. */
  let centred: string | null = null;
  let readerTouchedAt = 0;
  const READER_GRACE_MS = 2500;
  /* The offset we last wrote into a scroller, so the scroll event our own write
     fires is not mistaken for the reader's. */
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
    // the strip that is live depends on the mode: the vertical panel scrolls
    // its rows into view, the horizontal one its columns. Either way the node
    // is nudged toward the middle of the viewport, along the axis time runs.
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
  // The spy is an enhancement — it tells the minimap which message is on screen,
  // and a document with no minimap highlights nothing either way. Guarded rather
  // than assumed because this module is attached by the reading pane too, and a
  // runtime without IntersectionObserver must lose the highlight and not the
  // thread: a throw in here comes out of the renderer's own effect.
  const io = typeof IntersectionObserver === "function"
    ? new IntersectionObserver(
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
      )
    : null;
  if (io) cleanups.push(() => io.disconnect());
  for (const el of entries) {
    if (io) io.observe(el);
    on(el, "mouseenter", () => { if (nodeById.has(el.id)) { hovId = el.id; refresh(); } });
    on(el, "mouseleave", () => { if (hovId === el.id) { hovId = null; refresh(); } });
  }

  return () => { for (const c of cleanups) c(); };
}

/**
 * Click-to-enlarge for attachment thumbnails, and for pictures the sender put
 * in the body: one window over the page, still called `pop` after the popover it
 * grew out of. What it shows is the server's `view`: a picture, a block of text,
 * a framed PDF.
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
  let text: HTMLElement;
  let grid: HTMLElement;
  let frame: HTMLIFrameElement;
  let cap: HTMLElement;
  let note: HTMLElement;
  let save: HTMLAnchorElement;
  let closeBtn: HTMLButtonElement;
  let opener: HTMLElement | null = null;
  // Whatever a slow fetch was going to put in the window belongs to the opening
  // that asked for it. Closing, or opening something else, retires the number and
  // the reply is dropped: the reader has moved on, and bytes arriving late must
  // not paint over what they are looking at now.
  let opening = 0;
  let blobURL = "";

  /** Built on first use, so a page nobody enlarges anything on carries no extra DOM. */
  const build = () => {
    if (host) return;
    host = doc.createElement("div");
    host.className = "pop";
    host.setAttribute("role", "dialog");
    host.setAttribute("aria-modal", "true");
    // Named by its caption rather than by a fixed string: the window holds a
    // picture, a file's text or a framed document, and the one thing that is true
    // of all three is the file's name or the picture's alt text.
    host.setAttribute("aria-labelledby", "popcap");
    host.hidden = true;
    host.innerHTML =
      '<div class="popbox"><img class="popimg" alt="" hidden>' +
      '<pre class="poptext" hidden></pre>' +
      // A delimited file, read as cells instead of as lines of commas. Its own
      // element under the `pre` rather than a different `pre`: the text window
      // keeps `white-space:pre`, which a table must not inherit.
      '<div class="popgrid" hidden></div>' +
      // A frame holds the browser's own PDF viewer. Its type comes from the blob
      // this page makes out of the served bytes, never from the sender's claim,
      // which is the whole reason it is safe to frame.
      '<iframe class="popframe" title="" hidden></iframe>' +
      '<div class="popbar">' +
      '<span class="popcap" id="popcap"></span><span class="popnote"></span>' +
      '<a class="popget" download hidden>save</a>' +
      '<button type="button" class="popx">Close</button>' +
      "</div></div>";
    shot = host.querySelector<HTMLImageElement>(".popimg")!;
    text = host.querySelector<HTMLElement>(".poptext")!;
    grid = host.querySelector<HTMLElement>(".popgrid")!;
    frame = host.querySelector<HTMLIFrameElement>(".popframe")!;
    cap = host.querySelector<HTMLElement>(".popcap")!;
    note = host.querySelector<HTMLElement>(".popnote")!;
    save = host.querySelector<HTMLAnchorElement>(".popget")!;
    closeBtn = host.querySelector<HTMLButtonElement>(".popx")!;
    doc.body.appendChild(host);

    on(closeBtn, "click", close);
    // The backdrop is the host itself; a click that lands on the picture or the
    // bar must not dismiss, or dragging to select the caption closes the popover.
    on(host, "click", (ev: Event) => { if (ev.target === host) close(); });
    on(host, "keydown", (ev: Event) => {
      const k = ev as KeyboardEvent;
      // A framed document keeps its own keys — a PDF viewer swallows Escape and
      // Tab both — so this reaches the picture and text windows and the bar, and
      // Close and the backdrop are the way out of a PDF.
      if (k.key === "Escape") { k.preventDefault(); close(); return; }
      // Close and save are the only focusable things inside, so the trap is Tab
      // staying put rather than a cycle through a list. Written as a wrap anyway:
      // it stays correct with the save link showing as well. `hidden` is excluded
      // rather than left to fail, because a hidden element is in this list and
      // cannot take focus — the trap would land on nothing and Tab would stop
      // moving.
      if (k.key !== "Tab") return;
      const stops = [...host!.querySelectorAll<HTMLElement>(
        "button:not([hidden]), [href]:not([hidden])")];
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
    // Returning the keyboard where it came from: a reader who enlarged a picture
    // mid-transcript must not be dropped back at the top of the document.
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
    // A blob is held by the document until it is told otherwise, which for a
    // window the reader has closed is a file kept in memory for nothing.
    if (blobURL) { URL.revokeObjectURL(blobURL); blobURL = ""; }
  }

  /**
   * Enlarge one file over the page.
   *
   * A picture is shown from the bytes this host holds — the original, at its real
   * size — rather than from the preview the builder embedded, which is capped at
   * 640 pixels on its long edge and reads as a blur when it is blown up to fill a
   * screen. The preview is what is shown when those bytes cannot be fetched: the
   * corpus prunes bytes nothing points at, and a saved page can outlive them.
   *
   * Text and PDFs need the bytes in hand before they can be shown, so the window
   * is opened empty and filled when they arrive: text goes into a `pre` (so no
   * markup from a file ever becomes markup in the page), and a PDF becomes a blob
   * the browser will frame — the served URL is a download, and a frame handed an
   * attachment renders nothing.
   */
  const open = (
    from: HTMLElement, caption: string, preview: string, full: string, view: string,
  ) => {
    build();
    const mine = ++opening;
    empty();
    cap.textContent = caption;
    shot.alt = caption;
    frame.title = caption;
    // The save control, for a chip whose bytes this host holds. The window is
    // reached by clicking a chip, and that click is intercepted — so without this
    // there would be no route to the original file at all, only to what the page
    // can show of it. `download` is what makes it a save rather than a view: the
    // response is inline for a picture, and the attribute overrides that, with the
    // filename still coming from the Content-Disposition header.
    save.hidden = !full;
    if (full) save.href = full;
    if (view === "image") {
      // A 404 — bytes pruned since the page was saved — must not leave the window
      // empty-handed when there is a thumbnail to fall back to.
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
    // The overlay covers the viewport, so a pointer cannot reach the transcript
    // anyway; inert is what says the same thing to a screen reader and to the
    // tab order, which the Tab handler alone could only enforce for the keyboard.
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
        // A file longer than this is one to save and open elsewhere: a window is
        // for reading something, and a megabyte of log is not read by scrolling.
        // The bytes are already here, so the save control is right beside it.
        const cut = body.length > TEXT_CAP;
        const shown = cut ? body.slice(0, TEXT_CAP) : body;
        // A delimited file is drawn as the table it is — read off the bytes and
        // the served type, not off a field on the wire, so a page saved before
        // this existed gets its table too. Everything else stays text, and a file
        // that merely has commas in it (prose, a malformed export) is refused by
        // `readTable` and read as itself.
        const table = readTable(shown, caption, res.headers.get("content-type") ?? "");
        if (table) {
          drawTable(table);
          note.textContent = tableNote(table, cut);
          grid.hidden = false;
          return;
        }
        text.textContent = shown;
        // No units claimed: the cap is in characters and the file's size is on the
        // chip behind this window, so the honest thing to say here is that there
        // is more of it and where to get it.
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
      // A file that cannot be read says so in the window rather than leaving it
      // blank: the reader asked for it and is owed an answer. The save control is
      // still there, since the bytes may well save perfectly well.
      if (mine === opening) note.textContent = "could not be read here";
    }
  };

  /** Draw a delimited file as a table.
   *
   *  Built node by node and filled with `textContent`, exactly as the text window
   *  is: the cells are a sender's bytes, and a spreadsheet is not a reason to let
   *  any of it become markup. A rounded row is padded rather than broken, so a
   *  ragged export still lines up under its header. */
  const drawTable = (t: Table) => {
    const cell = (tag: "th" | "td", value: string, num: boolean) => {
      const el = doc.createElement(tag);
      el.textContent = value;
      if (num && tag === "td") el.className = "num";
      return el;
    };
    const table = doc.createElement("table");
    table.className = "poptable";
    const head = doc.createElement("thead");
    const hr = doc.createElement("tr");
    for (const [i, value] of t.header.entries()) hr.appendChild(cell("th", value, t.numeric[i] === true));
    head.appendChild(hr);
    table.appendChild(head);
    const body = doc.createElement("tbody");
    for (const row of t.rows) {
      const tr = doc.createElement("tr");
      for (const [i, value] of row.entries()) tr.appendChild(cell("td", value, t.numeric[i] === true));
      body.appendChild(tr);
    }
    table.appendChild(body);
    grid.textContent = "";
    grid.appendChild(table);
  };

  /** What the table window is not showing, when it is not showing all of it.
   *
   *  The same shape as the text window's note and for the same reason — a reader
   *  must know that a file continues — with the counts a table can give and a
   *  paragraph cannot: the rows and columns left out, then the fact that the
   *  bytes themselves were cut before the file ended. `save` is the way to the
   *  rest, and it is in the bar either way. */
  const tableNote = (t: Table, cut: boolean) => {
    const bits: string[] = [];
    if (t.rowCount > t.rows.length) bits.push(`first ${t.rows.length} of ${t.rowCount} rows`);
    if (t.colCount > t.header.length) bits.push(`first ${t.header.length} of ${t.colCount} columns`);
    if (cut) bits.push("truncated — save it for the rest");
    return bits.join(" · ");
  };

  /** Wire one chip up, once it is known to be worth opening.
   *
   *  `thumb` is the chip's embedded preview, when it has one: null for a small
   *  stored picture, whose bytes the corpus holds but never embedded.
   */
  const arm = (t: HTMLElement, thumb: HTMLImageElement | null, isChip: boolean) => {
    if (isChip && !thumb && !t.dataset.get) return;
    const label = isChip ? t.dataset.pop! : thumb?.getAttribute("alt") || "";
    // The keyboard has to reach whatever already takes focus. A chip is the link
    // itself; a body picture may be wrapped in one, and the wrapper is the tab
    // stop, so a listener on the picture would never see the Enter key.
    const trig: HTMLElement = isChip ? t : (t.closest("a") ?? t);
    if (trig === t && !isChip && !t.hasAttribute("tabindex")) t.tabIndex = 0;
    trig.setAttribute("aria-haspopup", "dialog");

    // Resolved at open time: the thumbnail is what is on screen to start with,
    // and the bytes this host holds are what the window actually shows. The view
    // is the renderer's — the server's for a chip, `image` for a body picture,
    // which is a picture by the fact that it is an img.
    const view = isChip ? trig.dataset.view || "image" : "image";
    const show = () =>
      open(trig, label, thumb ? thumb.currentSrc || thumb.src : "",
        isChip ? trig.dataset.get ?? "" : "", view);
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
    if (isChip) {
      // Armed by the renderer, which knows what this browser can be shown: the
      // embedded preview, or bytes this host holds. A small picture is the second
      // case — no preview, because the builder embeds one only above a size floor
      // — so the thumbnail is optional here rather than required, and its absence
      // is what used to leave these chips navigating away instead of popping up.
      arm(t, t.querySelector<HTMLImageElement>(".athumb"), true);
      continue;
    }
    const img = t as HTMLImageElement;
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
 * How much of a text file the window will hold, in characters.
 *
 * A window is for reading something, and a transcript, a CSV or a log past this
 * length is a file to save and open somewhere that scrolls properly. The bytes
 * are already here, so the save control sits right beside the truncated text; the
 * note says what was cut. Characters rather than bytes: this is a limit on what
 * goes into the DOM, and `text` is what comes out of the response.
 */
const TEXT_CAP = 256 * 1024;

/**
 * Whether a body picture is worth the window, or whether that cannot be told yet.
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
