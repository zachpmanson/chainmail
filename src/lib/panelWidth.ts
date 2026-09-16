/**
 * The width of the inbox's list column, as the reader dragged it.
 *
 * Kept in the browser rather than on the server, unlike the default folder. The
 * two are different kinds of preference: which folder chainmail opens in is
 * about the reader and is the same on every screen, while a panel width belongs
 * to the window it was dragged in — the proportion that suits a wide monitor is
 * the wrong one for a laptop — and on a narrow screen the list steps aside
 * entirely, so there is nothing there for a width to apply to. localStorage is
 * how the app already remembers the reply tree's layout (`cm-tree`), private
 * mode included: a browser that refuses to store it still gets a splitter, it
 * just forgets between visits.
 */

/** The narrowest the list may be dragged. The same 15rem the grid allowed
 *  before the splitter existed, so dragging to the edge cannot make the list
 *  narrower than the layout already said was readable. */
export const LIST_MIN = 15 * 16;

/** What the reading pane keeps. A thread is what is being read, so the list may
 *  not be dragged over it: past this the drag stops, rather than the pane
 *  becoming a column of wrapped words. */
export const PANE_MIN = 26 * 16;

/** The step one arrow key takes. Small enough to land on the width you meant,
 *  large enough not to need twenty presses. */
export const LIST_STEP = 16;

const KEY = "cm-list";

/** The stored width, or null when the reader has never dragged. Null is not the
 *  same as a number: it means the layout's own width, which is a maximum the CSS
 *  picks for the screen rather than a width anyone chose. */
export function readListWidth(): number | null {
  try {
    const raw = localStorage.getItem(KEY);
    if (raw === null) return null;
    const px = Number.parseInt(raw, 10);
    return Number.isFinite(px) ? clampListWidth(px, Number.POSITIVE_INFINITY) : null;
  } catch {
    return null;
  }
}

/** Remember the width, or forget it entirely for null — which is how the reader
 *  gets the default back. */
export function rememberListWidth(px: number | null): void {
  try {
    if (px === null) localStorage.removeItem(KEY);
    else localStorage.setItem(KEY, String(Math.round(px)));
  } catch {
    /* private mode: the splitter still works, the choice just does not last */
  }
}

/** Hold a dragged width inside what both columns can live with. The container is
 *  the whole split, so what the pane is left with is the container minus the
 *  list and the handle between them. */
export function clampListWidth(px: number, container: number): number {
  const most = Math.max(LIST_MIN, container - PANE_MIN);
  return Math.round(Math.min(Math.max(px, LIST_MIN), most));
}
