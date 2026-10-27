import { expect, it } from "vitest";
import { usePrefs } from "../src/lib/prefs/usePrefs";
import { appUndo } from "../src/lib/ui/undo";

it("undoes compact and reply-tree choices without restoring unrelated preferences", async () => {
  usePrefs.setState({ compact: false, tree: false, listWidth: 300 });
  usePrefs.getState().setCompact(true);
  usePrefs.getState().setTree(true);
  await appUndo.perform(() => undefined);
  expect(usePrefs.getState().compact).toBe(true);
  expect(usePrefs.getState().tree).toBe(true);
  usePrefs.getState().setListWidth(400);
  await appUndo.undo();
  expect(usePrefs.getState().tree).toBe(false);
  expect(usePrefs.getState().compact).toBe(true);
  await appUndo.undo();
  expect(usePrefs.getState().compact).toBe(false);
  expect(usePrefs.getState().listWidth).toBe(400);
  usePrefs.getState().setCompact(false);
  await appUndo.perform(() => undefined);
  expect(appUndo.getSnapshot().label).toBe("");
});
