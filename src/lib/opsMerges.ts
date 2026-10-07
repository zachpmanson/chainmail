import type { OpsMerge } from "./api";

export type MergeTarget = Pick<OpsMerge, "keepId" | "dropId">;

export type MergeBatchResult = {
  merged: number;
  total: number;
  refusal?: { target: MergeTarget; error: unknown };
};

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
