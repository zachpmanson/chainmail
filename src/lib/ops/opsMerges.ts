import { useQueryClient } from "@tanstack/react-query";
import { useReducer } from "react";
import { $api, type OpsMerge } from "../api/api";

export type MergeTarget = Pick<OpsMerge, "keepId" | "dropId">;

export type MergeBatchResult = {
  merged: number;
  total: number;
  refusal?: { target: MergeTarget; error: unknown };
};

function errText(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

/** The dedupe rules, labelled for a screen the CLI naming would obscure. */
const RULES: Record<string, string> = {
  "dedupe:same-display-name": "same display name",
  "dedupe:same-display-name-in-thread": "same display name, in thread",
  "dedupe:first-name-and-org": "first name at one organisation",
  "dedupe:webmail-and-work-mailbox": "webmail and work mailbox",
};
export const ruleLabel = (rule: string): string => RULES[rule] ?? rule;

/** Apply selected pairs in order, stopping on the first server refusal. */
export async function applyMergeBatch(
  batch: MergeTarget[],
  apply: (target: MergeTarget) => Promise<unknown>,
  onProgress: (progress: string) => void,
  onMerged: (dropId: number) => void,
): Promise<MergeBatchResult> {
  let merged = 0;
  for (const target of batch) {
    onProgress(`${merged + 1} of ${batch.length}`);
    try {
      await apply(target);
    } catch (error) {
      return { merged, total: batch.length, refusal: { target, error } };
    }
    merged += 1;
    onMerged(target.dropId);
  }
  return { merged, total: batch.length };
}

type MergeRun = {
  phase:
    { kind: "picking" } | { kind: "confirming" } | { kind: "merging"; progress: string | null };
  // Ticked pairs, by the id of the person they drop (unique in a plan).
  selected: Set<number>;
  // Hides merged pairs until the plan refetch, which drops them, lands.
  applied: Set<number>;
  error: string | null;
  last: string | null;
};

type MergeRunAction =
  | { type: "pick"; dropId: number; picked: boolean }
  | { type: "pickAll"; dropIds: number[] }
  | { type: "confirm" }
  | { type: "cancel" }
  | { type: "start" }
  | { type: "progress"; progress: string }
  | { type: "merged"; dropId: number }
  | { type: "finish"; merged: number; error: string | null };

function mergeRunReducer(state: MergeRun, action: MergeRunAction): MergeRun {
  switch (action.type) {
    case "pick": {
      const selected = new Set(state.selected);
      if (action.picked) selected.add(action.dropId);
      else selected.delete(action.dropId);
      return { ...state, selected };
    }
    case "pickAll":
      return { ...state, selected: new Set(action.dropIds) };
    case "confirm":
      return { ...state, phase: { kind: "confirming" }, error: null };
    case "cancel":
      return { ...state, phase: { kind: "picking" } };
    case "start":
      return { ...state, phase: { kind: "merging", progress: null }, error: null };
    case "progress":
      return { ...state, phase: { kind: "merging", progress: action.progress } };
    case "merged":
      return { ...state, applied: new Set(state.applied).add(action.dropId) };
    case "finish":
      return {
        ...state,
        phase: { kind: "picking" },
        selected: new Set(),
        error: action.error ?? state.error,
        last:
          action.merged === 0
            ? state.last
            : action.merged === 1
              ? "merged 1 pair"
              : `merged ${action.merged} pairs`,
      };
  }
}

/** The Merges tab's pick → confirm → merge loop over the plan's pairs. */
export function useMergeRun(planned: OpsMerge[] | undefined) {
  const qc = useQueryClient();
  const apply = $api.useMutation("post", "/v1/ops/merge");
  const [state, dispatch] = useReducer(mergeRunReducer, {
    phase: { kind: "picking" },
    selected: new Set<number>(),
    applied: new Set<number>(),
    error: null,
    last: null,
  });
  const { selected, applied } = state;

  // Applicable pairs first: they are what this screen exists to act on.
  const merges = planned
    ? [...planned]
        .filter((m) => !applied.has(m.dropId))
        .sort((a, b) => Number(b.applicable) - Number(a.applicable))
    : [];
  const applicable = merges.filter((m) => m.applicable);
  const chosen = merges.filter((m) => selected.has(m.dropId));
  const allPicked = applicable.length > 0 && applicable.every((m) => selected.has(m.dropId));

  /** Sequential on purpose: each POST re-derives the plan server-side, so a pair an
   *  earlier merge made moot comes back as a 409 and the rest are not sent. */
  async function run() {
    const batch = chosen;
    dispatch({ type: "start" });
    const result = await applyMergeBatch(
      batch,
      (target) => apply.mutateAsync({ body: { keepId: target.keepId, dropId: target.dropId } }),
      (progress) => dispatch({ type: "progress", progress }),
      (dropId) => dispatch({ type: "merged", dropId }),
    );
    const refusal = result.refusal;
    dispatch({
      type: "finish",
      merged: result.merged,
      error: refusal
        ? `${result.merged} of ${result.total} merged, then folding #${refusal.target.dropId} into #${refusal.target.keepId} ` +
          `was refused: ${errText(refusal.error)}`
        : null,
    });
    await qc.invalidateQueries({ queryKey: ["get", "/v1/ops/plan"] });
  }

  return {
    phase: state.phase,
    busy: state.phase.kind === "merging",
    selected,
    error: state.error,
    last: state.last,
    merges,
    applicable,
    chosen,
    allPicked,
    pick: (dropId: number, picked: boolean) => dispatch({ type: "pick", dropId, picked }),
    pickAll: (picked: boolean) =>
      dispatch({ type: "pickAll", dropIds: picked ? applicable.map((m) => m.dropId) : [] }),
    confirm: () => dispatch({ type: "confirm" }),
    cancel: () => dispatch({ type: "cancel" }),
    run,
  };
}
