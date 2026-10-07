import { create } from "zustand";
import { createJSONStorage, persist, type StateStorage } from "zustand/middleware";
import { clampListWidth } from "./panelWidth";

// Stored per browser, not on the server: a good list width depends on the screen.

type Saved = {
  compact: boolean;
  /** Reply-tree layout in the thread pane. */
  tree: boolean;
  /** null means never dragged: use the layout's CSS default. */
  listWidth: number | null;
  /**
   * Senders whose mail is read as they wrote it. A local fallback only: the corpus's
   * people.prefer_original is the store of record; this serves built pages and quote-recovered messages.
   */
  styledSenders: string[];
};

const COMPACT_KEY = "cm-compact";
// Distinct from "cm-tree", which behaviour.ts uses for the page view's tree layout.
const TREE_KEY = "cm-treeview";
const LIST_KEY = "cm-list";
const STYLED_KEY = "chainmail:styled-senders";

/** Persist's one blob, spread over the keys and formats the earlier hand-rolled storage used. */
function legacyKeys(store: Storage): StateStorage {
  return {
    getItem: () => {
      const width = Number.parseInt(store.getItem(LIST_KEY) ?? "", 10);
      const state: Saved = {
        compact: store.getItem(COMPACT_KEY) === "1",
        tree: store.getItem(TREE_KEY) === "1",
        listWidth: Number.isFinite(width) ? clampListWidth(width, Number.POSITIVE_INFINITY) : null,
        styledSenders: senders(store.getItem(STYLED_KEY)),
      };
      return JSON.stringify({ state, version: 0 });
    },
    setItem: (_, blob) => {
      const { state } = JSON.parse(blob) as { state: Saved };
      store.setItem(COMPACT_KEY, state.compact ? "1" : "0");
      store.setItem(TREE_KEY, state.tree ? "1" : "0");
      if (state.listWidth === null) store.removeItem(LIST_KEY);
      else store.setItem(LIST_KEY, String(state.listWidth));
      store.setItem(STYLED_KEY, JSON.stringify(state.styledSenders));
    },
    removeItem: () => {
      for (const key of [COMPACT_KEY, TREE_KEY, LIST_KEY, STYLED_KEY]) store.removeItem(key);
    },
  };
}

function senders(held: string | null): string[] {
  if (!held) return [];
  let parsed: unknown;
  try {
    parsed = JSON.parse(held);
  } catch {
    return []; // Garbage from an older build reads as no senders.
  }
  return Array.isArray(parsed)
    ? parsed.filter((key): key is string => typeof key === "string" && key !== "")
    : [];
}

export const usePrefs = create<
  Saved & {
    setCompact: (compact: boolean) => void;
    setTree: (tree: boolean) => void;
    setListWidth: (px: number | null) => void;
    toggleStyled: (sender: string) => void;
  }
>()(
  persist(
    (set) => ({
      compact: false,
      tree: false,
      listWidth: null,
      styledSenders: [],
      setCompact: (compact) => set({ compact }),
      setTree: (tree) => set({ tree }),
      setListWidth: (px) => {
        if (px !== null && !Number.isFinite(px))
          throw new Error(`list width ${px} is not a number`);
        set({ listWidth: px === null ? null : clampListWidth(px, Number.POSITIVE_INFINITY) });
      },
      // A recovered entry with neither sender nor id has nothing to key the switch on.
      toggleStyled: (sender) => {
        if (sender === "") return;
        set(({ styledSenders }) => ({
          styledSenders: styledSenders.includes(sender)
            ? styledSenders.filter((s) => s !== sender)
            : [...styledSenders, sender],
        }));
      },
    }),
    {
      // Unused by legacyKeys, which keeps each preference under its own key.
      name: "cm-prefs",
      storage: createJSONStorage(() => legacyKeys(window.localStorage)),
      partialize: ({ compact, tree, listWidth, styledSenders }): Saved => ({
        compact,
        tree,
        listWidth,
        styledSenders,
      }),
    },
  ),
);
