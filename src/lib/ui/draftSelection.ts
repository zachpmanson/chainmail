import type { components } from "../api/api.d";
import { COMPOSE_OPEN } from "./openSelection";

export type SavedDraft = components["schemas"]["DraftResponse"];

/** IDs are opaque: validate without changing their identity. */
export function draftId(value: unknown): string | undefined {
  return typeof value === "string" && value.trim() ? value : undefined;
}

export function draftDestination(draft: SavedDraft): string | undefined {
  if (!draftId(draft.accountId) || !draftId(draft.id)) return undefined;
  if (draft.kind === "compose" && draft.resolution === "compose") return COMPOSE_OPEN;
  if (
    draft.kind === "reply" &&
    draft.resolution === "resolved" &&
    draftId(draft.rootId) &&
    draftId(draft.replyToEntry)
  )
    return draft.rootId;
  return undefined;
}

export function validatedDraft(draft: SavedDraft, account: string, id: string): boolean {
  return draft.accountId === account && draft.id === id && Boolean(draftDestination(draft));
}

export function draftIdentity(account: string, id: string): string {
  return JSON.stringify([account, id]);
}
