import { useEffect, useSyncExternalStore } from "react";
import { ArrowUturnLeftIcon } from "@heroicons/react/24/outline";
import IconButton from "../ui/IconButton";
import { appUndo, isUndoKey } from "../../lib/ui/undo";
import { pushToast } from "../../lib/ui/toasts";
import { errText } from "../../lib/ui/errText";

export default function NavUndo() {
  const state = useSyncExternalStore(appUndo.subscribe, appUndo.getSnapshot);
  const undo = () => {
    void appUndo
      .undo()
      .then((label) => {
        if (label) pushToast(`Undid ${label}`, "note", 5000);
      })
      .catch((error: unknown) => {
        pushToast(`Undo failed: ${errText(error)}. The action stays in undo history.`, "fail");
      });
  };
  useEffect(() => {
    if (typeof document === "undefined") return;
    const key = (event: KeyboardEvent) => {
      if (!isUndoKey(event, event.composedPath())) return;
      const state = appUndo.getSnapshot();
      if (!state.label && !state.pending) return;
      event.preventDefault();
      undo();
    };
    document.addEventListener("keydown", key);
    return () => document.removeEventListener("keydown", key);
  }, []);
  return (
    <IconButton
      aria-label="Undo"
      title={`Undo${state.label ? ` ${state.label}` : ""} (Ctrl+Z / ⌘Z)`}
      disabled={!state.label && !state.pending}
      onClick={undo}
    >
      <ArrowUturnLeftIcon aria-hidden="true" />
    </IconButton>
  );
}
