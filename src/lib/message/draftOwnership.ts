import type { QueryClient } from "@tanstack/react-query";
import type { components } from "../api/api.d";

type SavedDraft = components["schemas"]["DraftResponse"];

/** Dirty editors and completed drafts cannot be claimed by a late list response. */
export default class DraftOwnership {
  private scope = "";
  private dirty = false;
  private retired = new Set<string>();

  enter(scope: string, hasWords: boolean) {
    if (scope !== this.scope) {
      this.scope = scope;
      this.dirty = hasWords;
    }
  }

  edit() {
    this.dirty = true;
  }

  canHydrate() {
    return !this.dirty;
  }

  isRetired(account: string, id: string) {
    return this.retired.has(JSON.stringify([account, id]));
  }

  retire(account: string, id: string) {
    this.retired.add(JSON.stringify([account, id]));
  }
}

export function removeCachedDraft(client: QueryClient, account: string, id: string) {
  client.setQueriesData<SavedDraft[]>({ queryKey: ["get", "/v1/drafts"] }, (drafts) =>
    drafts?.filter((draft) => draft.accountId !== account || draft.id !== id),
  );
}
