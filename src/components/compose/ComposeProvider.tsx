import type { SearchParams } from "../../router";
import useThreadPopouts from "./useThreadPopouts";
import { ThreadPopoutContext } from "./ThreadPopoutContext";
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { useBlocker, useNavigate, useRouter, useRouterState } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { $api } from "../../lib/api/api";
import {
  draftDestination,
  draftId,
  draftIdentity,
  validatedDraft,
  type SavedDraft,
} from "../../lib/ui/draftSelection";
import { removeCachedDraft } from "../../lib/message/draftOwnership";
import { ComposeContext, type ComposeContextValue } from "./ComposeContext";
import {
  DraftSelectionContext,
  type DraftSelection,
  type LeaveSnapshot,
} from "./DraftSelectionContext";
import InlineAlert from "../ui/InlineAlert";
import Button from "../ui/Button";

function Detail({
  account,
  id,
  admit,
  prepared,
}: {
  account: string;
  id: string;
  admit: (draft?: SavedDraft, error?: string) => void;
  prepared?: SavedDraft;
}) {
  const detail = $api.useQuery(
    "get",
    "/v1/drafts/{id}",
    { params: { path: { id }, query: { accountId: account } } },
    { enabled: !prepared, retry: false, refetchOnWindowFocus: false, staleTime: 0 },
  );
  useEffect(() => {
    if (prepared) admit(prepared);
    else if (detail.isError)
      admit(undefined, "This draft could not be read. Clear selection and try again.");
    else if (detail.data && !detail.isFetching) {
      if (validatedDraft(detail.data, account, id)) admit(detail.data);
      else
        admit(
          undefined,
          "This draft has no valid destination, or returned a different account or ID.",
        );
    }
  }, [account, id, detail.data, detail.isError, detail.isFetching, admit, prepared]);
  return null;
}

// Match Home's component boundaries, not every search/open change.
function editorHost(search: SearchParams) {
  if (search.popup === "1" && search.open !== "compose") return "popup";
  return search.q?.trim() || search.person?.trim() || search.since?.trim() ? "select" : "inbox";
}
function admissionIdentity(pathname: string, search: SearchParams) {
  return JSON.stringify([
    pathname,
    editorHost(search),
    search.draft !== undefined,
    draftId(search.accountId),
    draftId(search.draft),
  ]);
}

export default function ComposeProvider({
  children,
  composing,
  closeCompose,
}: ComposeContextValue & { children: ReactNode }) {
  const navigate = useNavigate();
  const router = useRouter();
  const location = useRouterState({ select: (s) => s.location });
  const client = useQueryClient();
  const search = location.pathname === "/" ? location.search : {};
  const active = search.draft !== undefined;
  const account = draftId(search.accountId);
  const id = draftId(search.draft);
  const identity = admissionIdentity(location.pathname, search);
  const request = useRef({ identity, epoch: 1 });
  if (request.current.identity !== identity)
    request.current = { identity, epoch: request.current.epoch + 1 };
  const epoch = request.current.epoch;
  const [result, setResult] = useState<{ epoch: number; draft?: SavedDraft; error?: string }>();
  const retired = useRef(new Set<string>());
  const editor = useRef<(() => LeaveSnapshot) | undefined>();
  const adoption = useRef<{
    identity: string;
    epoch: number;
    editor: () => LeaveSnapshot;
    kind: string;
    destination: string;
    target?: string;
  }>();
  const approvedSelection = useRef<{
    identity: string;
    epoch: number;
    editor?: () => LeaveSnapshot;
    revision?: string;
  }>();
  const canonicalNavigation = useRef<{ epoch: number; destination: string }>();
  const [notice, setNotice] = useState("");
  const preparedOpening = useRef<{ epoch: number; draft: SavedDraft }>();
  const seenPreparation = useRef<unknown>();
  const popouts = useThreadPopouts({
    editor,
    incoming:
      search.popup === "1" && search.handoff !== undefined
        ? {
            token: search.handoff,
            root: search.open ?? "",
            account: search.accountId ?? "",
            id: search.draft,
          }
        : undefined,
    canRestore: () => !editor.current,
    restore: async (returned) => {
      if (editor.current) throw new Error("Another editor is open");
      request.current = { identity: "", epoch: request.current.epoch + 1 };
      setResult(undefined);
      adoption.current = undefined;
      approvedSelection.current = undefined;
      client.removeQueries({ queryKey: ["get", "/v1/drafts/{id}"] });
      // Clear admission first, even when the returned ID equals the original ID.
      await navigate({
        to: "/",
        search: (prev) => ({ ...prev, draft: undefined }),
        replace: true,
        ignoreBlocker: true,
      });
      await navigate({
        to: "/",
        search: (prev) => ({
          ...prev,
          open: returned.root,
          accountId: returned.account,
          draft: returned.id,
          popup: undefined,
          handoff: undefined,
        }),
        replace: true,
        ignoreBlocker: true,
      });
      const current = router.state.location;
      if (
        current.pathname !== "/" ||
        current.search.open !== returned.root ||
        current.search.accountId !== returned.account ||
        current.search.draft !== returned.id
      )
        throw new Error("Return navigation did not complete");
      // Only this admission may use the checked snapshot; later reopenings read afresh.
      request.current = {
        identity: admissionIdentity(current.pathname, current.search),
        epoch: request.current.epoch + 1,
      };
      preparedOpening.current = popouts.prepared?.draft
        ? { epoch: request.current.epoch, draft: popouts.prepared.draft }
        : undefined;
      setResult(undefined);
    },
  });
  if (popouts.prepared !== seenPreparation.current) {
    seenPreparation.current = popouts.prepared;
    preparedOpening.current = popouts.prepared?.draft
      ? { epoch, draft: popouts.prepared.draft }
      : undefined;
  }
  const accounts = $api.useQuery("get", "/auth/status", {});
  const connected = Boolean(
    account && accounts.data?.accounts.some((a) => a.id === account && a.signedIn),
  );
  const isRetired = (a: string, d: string) => retired.current.has(draftIdentity(a, d));
  const approve = useCallback(() => {
    const snapshot = editor.current?.();
    if (snapshot?.busy || popouts.moving.current) {
      setNotice("Wait for the current draft operation before leaving.");
      return false;
    }
    if (
      snapshot?.dirty &&
      !window.confirm(
        "Leave this editor and discard its local input/options? Check Gmail before abandoning a stopped or uncertain save.",
      )
    )
      return false;
    snapshot?.invalidate();
    setNotice("");
    return true;
  }, [popouts.moving]);
  useBlocker({
    shouldBlockFn: ({ current, next }) => {
      if (
        current.pathname === next.pathname &&
        JSON.stringify(current.search) === JSON.stringify(next.search)
      )
        return false;
      const currentSearch = current.search as SearchParams;
      const nextSearch = next.search as SearchParams;
      const scope = popouts.scope;
      if (
        scope &&
        (next.pathname !== "/" ||
          nextSearch.popup !== "1" ||
          nextSearch.handoff !== scope.token ||
          nextSearch.open !== scope.root ||
          nextSearch.accountId !== scope.account)
      ) {
        setNotice("Use Return to main window before leaving this managed thread window.");
        return true;
      }
      const snapshot = editor.current?.();
      const sameView =
        current.pathname === next.pathname &&
        ["q", "person", "since", "popup"].every(
          (key) =>
            current.search[key as keyof typeof current.search] ===
            next.search[key as keyof typeof next.search],
        );
      // Adopting the editor's own autosaved ID changes only its address, not its buffer.
      if (
        sameView &&
        snapshot?.owner &&
        snapshot.kind &&
        snapshot.destination &&
        snapshot.destination === nextSearch.open &&
        snapshot.owner === draftIdentity(nextSearch.accountId ?? "", nextSearch.draft ?? "") &&
        currentSearch.open === nextSearch.open &&
        editor.current
      ) {
        adoption.current = {
          identity: admissionIdentity(next.pathname, nextSearch),
          epoch:
            request.current.epoch +
            (admissionIdentity(next.pathname, nextSearch) === identity ? 0 : 1),
          editor: editor.current,
          kind: snapshot.kind,
          destination: snapshot.destination,
          target: snapshot.target,
        };
        return false;
      }
      const approved = approvedSelection.current;
      const canonical = canonicalNavigation.current;
      canonicalNavigation.current = undefined;
      if (
        canonical?.epoch === request.current.epoch &&
        nextSearch.open === canonical.destination &&
        current.pathname === next.pathname &&
        JSON.stringify({ ...currentSearch, open: undefined }) ===
          JSON.stringify({ ...nextSearch, open: undefined }) &&
        approved?.identity === identity &&
        approved.epoch === epoch &&
        approved.editor === editor.current &&
        approved.revision === snapshot?.revision &&
        !snapshot?.busy
      ) {
        approvedSelection.current = undefined;
        return false;
      }
      if (!approve()) return true;
      approvedSelection.current = nextSearch.draft
        ? {
            identity: admissionIdentity(next.pathname, nextSearch),
            epoch:
              request.current.epoch +
              (admissionIdentity(next.pathname, nextSearch) === identity ? 0 : 1),
            editor: editor.current,
            revision: editor.current?.().revision,
          }
        : undefined;
      return false;
    },
    enableBeforeUnload: false,
  });
  useEffect(() => {
    const unload = (event: BeforeUnloadEvent) => {
      const snapshot = editor.current?.();
      if (!snapshot?.busy && !snapshot?.dirty) return;
      event.preventDefault();
      event.returnValue = "";
    };
    window.addEventListener("beforeunload", unload);
    return () => window.removeEventListener("beforeunload", unload);
  }, []);
  const admit = useCallback(
    (draft?: SavedDraft, error?: string) => {
      if (request.current.epoch !== epoch) return;
      const own = adoption.current;
      if (
        draft &&
        own?.identity === identity &&
        own.epoch === epoch &&
        own.editor === editor.current &&
        (draft.kind !== own.kind ||
          draftDestination(draft) !== own.destination ||
          (own.kind === "reply" && draft.replyToEntry !== own.target))
      ) {
        draft = undefined;
        error =
          "This draft no longer matches the current editor. Clear selection before opening it again.";
      }
      setResult((old) => (old?.epoch === epoch ? old : { epoch, draft, error }));
    },
    [epoch, identity],
  );
  const resolved = result?.epoch === epoch ? result : undefined;
  const unavailable = Boolean(account && id && isRetired(account, id));
  const destination = resolved?.draft && draftDestination(resolved.draft);
  const [canonicalAttempt, setCanonicalAttempt] = useState(0);
  const attempted = useRef("");
  useEffect(() => {
    if (!active || unavailable || !destination || search.open === destination) return;
    const key = JSON.stringify([epoch, canonicalAttempt]);
    if (attempted.current === key) return;
    attempted.current = key;
    // Only unchanged, already-approved input may reuse its departure approval.
    canonicalNavigation.current = { epoch, destination };
    void navigate({ to: "/", search: (prev) => ({ ...prev, open: destination }), replace: true });
  }, [active, unavailable, destination, search.open, epoch, canonicalAttempt, navigate]);
  const clear = () =>
    void navigate({ to: "/", search: (prev) => ({ ...prev, draft: undefined }), replace: true });
  const error = active
    ? !account || !id
      ? "A draft link requires an explicit account ID and draft ID."
      : unavailable
        ? "This draft was sent or discarded in this session."
        : accounts.isError
          ? "Connected accounts could not be checked."
          : accounts.data && !connected
            ? "This draft's account is disconnected or missing."
            : resolved?.error
    : undefined;
  const selection: DraftSelection = {
    epoch,
    active,
    loading: Boolean(active && !error && !destination),
    accountId: account,
    id,
    adopting:
      adoption.current?.identity === identity &&
      adoption.current.epoch === epoch &&
      adoption.current.editor === editor.current
        ? adoption.current
        : undefined,
    draft: !unavailable && connected && destination === search.open ? resolved?.draft : undefined,
  };
  return (
    <ComposeContext.Provider value={{ composing, closeCompose }}>
      <ThreadPopoutContext.Provider value={popouts}>
        <DraftSelectionContext.Provider
          value={{
            selection,
            clear,
            editor,
            approve,
            isRetired,
            select: (draft) => {
              const open = draftDestination(draft);
              if (!open || isRetired(draft.accountId, draft.id)) return;
              const current = router.buildLocation({ search: true, hash: true, state: true });
              const currentSearch = current.search as SearchParams;
              if (
                current.pathname === "/" &&
                currentSearch.accountId === draft.accountId &&
                currentSearch.draft === draft.id &&
                currentSearch.open === open
              )
                return;
              const snapshot = editor.current?.();
              // A proven own reply must keep its host as well as its target and buffer.
              const preservePopup =
                current.pathname === "/" &&
                currentSearch.open === open &&
                draft.kind === "reply" &&
                snapshot?.kind === "reply" &&
                snapshot.destination === open &&
                Boolean(snapshot.target) &&
                snapshot.target === draft.replyToEntry &&
                snapshot.owner === draftIdentity(draft.accountId, draft.id);
              void navigate({
                to: "/",
                search: {
                  ...current.search,
                  accountId: draft.accountId,
                  draft: draft.id,
                  open,
                  popup: popouts.managed || preservePopup ? currentSearch.popup : undefined,
                },
              });
            },
            detach: (newAccount) => {
              if (popouts.managed && newAccount !== popouts.scope?.account) {
                setNotice("Return to main window before changing this thread's account.");
                return false;
              }
              if (editor.current?.().busy) {
                setNotice("Wait for the current draft operation before changing account.");
                return false;
              }
              void navigate({
                to: "/",
                search: (prev) => ({ ...prev, accountId: newAccount, draft: undefined }),
                replace: true,
                ignoreBlocker: true,
              });
              return true;
            },
            retire: (a, d) => {
              retired.current.add(draftIdentity(a, d));
              removeCachedDraft(client, a, d);
              client.removeQueries({
                queryKey: [
                  "get",
                  "/v1/drafts/{id}",
                  { params: { path: { id: d }, query: { accountId: a } } },
                ],
              });
              const current = router.state.location;
              if (
                request.current.epoch === epoch &&
                current.pathname === "/" &&
                current.search.accountId === a &&
                current.search.draft === d
              )
                void navigate({
                  to: "/",
                  search: (prev) => ({ ...prev, draft: undefined }),
                  replace: true,
                  ignoreBlocker: true,
                });
            },
          }}
        >
          {active && account && id && connected && !unavailable ? (
            <Detail
              key={JSON.stringify([account, id, epoch])}
              account={account}
              id={id}
              admit={admit}
              prepared={
                preparedOpening.current?.epoch === epoch &&
                preparedOpening.current.draft.id === id &&
                preparedOpening.current.draft.accountId === account
                  ? preparedOpening.current.draft
                  : undefined
              }
            />
          ) : null}
          {notice ? <InlineAlert>{notice}</InlineAlert> : null}
          {active && !selection.draft && (error || destination) ? (
            <InlineAlert>
              {error || "The draft's destination requires approval."}{" "}
              {destination && !error ? (
                <Button
                  type="button"
                  density="compact"
                  onClick={() => setCanonicalAttempt((n) => n + 1)}
                >
                  Open destination
                </Button>
              ) : null}
              <Button type="button" density="compact" onClick={clear}>
                Clear draft selection
              </Button>
            </InlineAlert>
          ) : null}
          {popouts.message ? <InlineAlert>{popouts.message}</InlineAlert> : null}
          {popouts.returnToMain ? (
            <Button type="button" onClick={popouts.returnToMain}>
              Return to main window
            </Button>
          ) : null}
          {popouts.hidden ? null : children}
        </DraftSelectionContext.Provider>
      </ThreadPopoutContext.Provider>
    </ComposeContext.Provider>
  );
}
