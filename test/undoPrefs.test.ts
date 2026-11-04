import { expect, it, vi } from "vitest";
import { usePrefs } from "../src/lib/prefs/usePrefs";
import { appUndo } from "../src/lib/ui/undo";

it("applies layout preferences immediately without creating undo entries", () => {
  usePrefs.setState({ compact: false, tree: false, listWidth: 300 });
  usePrefs.getState().setCompact(true);
  usePrefs.getState().setTree(true);
  expect(usePrefs.getState().compact).toBe(true);
  expect(usePrefs.getState().tree).toBe(true);
  expect(appUndo.getSnapshot()).toEqual({ label: "", pending: false });
  usePrefs.getState().setCompact(false);
  usePrefs.getState().setTree(false);
  expect(appUndo.getSnapshot()).toEqual({ label: "", pending: false });
});

it("never waits behind mailbox work or replaces its undo entry", async () => {
  const undoArchive = vi.fn();
  await appUndo.perform(() => appUndo.remember({ label: "archive", undo: undoArchive }));
  let release!: () => void;
  const held = appUndo.perform(() => new Promise<void>((resolve) => (release = resolve)));
  await Promise.resolve();
  try {
    usePrefs.getState().setCompact(true);
    usePrefs.getState().setTree(true);
    usePrefs.getState().setListWidth(400);
    expect(usePrefs.getState().compact).toBe(true);
    expect(usePrefs.getState().tree).toBe(true);
    expect(appUndo.getSnapshot()).toEqual({ label: "archive", pending: true });
  } finally {
    release();
    await held;
  }
  await appUndo.undo();
  expect(undoArchive).toHaveBeenCalledOnce();
  expect(usePrefs.getState().compact).toBe(true);
  expect(usePrefs.getState().tree).toBe(true);
  expect(usePrefs.getState().listWidth).toBe(400);
  expect(appUndo.getSnapshot().label).toBe("");
  usePrefs.getState().setCompact(false);
  usePrefs.getState().setTree(false);
});
