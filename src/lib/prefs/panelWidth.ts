export const LIST_MIN = 15 * 16;

export const PANE_MIN = 26 * 16;

export const LIST_STEP = 16;

export function clampListWidth(px: number, container: number): number {
  const most = Math.max(LIST_MIN, container - PANE_MIN);
  return Math.round(Math.min(Math.max(px, LIST_MIN), most));
}
