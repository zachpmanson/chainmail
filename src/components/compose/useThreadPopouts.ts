import { useCallback, useEffect, useRef, useState, type MutableRefObject } from "react";
import { flushSync } from "react-dom";
import { fetchClient } from "../../lib/api/api";
import { validatedDraft } from "../../lib/ui/draftSelection";
import { gmailIdOf } from "../../lib/message/sources";
import { hoistEdits } from "../../lib/thread/hoist";
import type { LeaveSnapshot } from "./DraftSelectionContext";
import type { PopoutLocation, PreparedPopout, ThreadPopouts } from "./ThreadPopoutContext";

const WAIT = 10000;
const channelName = (token: string) => `chainmail-popout-${token}`;
type Packet = { type: string; location?: PopoutLocation; attempt?: string };
type Away = {
  location: PopoutLocation;
  channel: BroadcastChannel;
  committed: boolean;
  returning?: PreparedPopout;
  attempt?: string;
  admitting?: boolean;
};
const same = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b);
function validLocation(value: unknown): value is PopoutLocation {
  if (!value || typeof value !== "object") return false;
  const item = value as Record<string, unknown>;
  const text = (part: unknown) => typeof part === "string" && part.length > 0;
  return Boolean(
    text(item.root) &&
    (item.mode === "reader"
      ? typeof item.account === "string" &&
        item.target === undefined &&
        item.id === undefined &&
        item.messageId === undefined
      : item.mode === "reply" &&
        text(item.account) &&
        text(item.target) &&
        (item.id === undefined
          ? item.messageId === undefined
          : text(item.id) && text(item.messageId))),
  );
}

/** Fresh, revision-bound preparation. Content stays in destination memory, never in packets. */
async function prepare(location: PopoutLocation, fresh = false): Promise<PreparedPopout> {
  const chain = await fetchClient.GET("/v1/chains/{rootExtId}", {
    params: {
      path: { rootExtId: location.root },
      query: { accountId: location.account || undefined },
    },
    cache: "no-store",
  });
  if (chain.error || !chain.data) throw new Error();
  if (location.mode === "reader") {
    if (hoistEdits(chain.data.entries).shown.some((entry) => gmailIdOf(entry) !== undefined))
      throw new Error();
  } else if (
    !chain.data.entries.some(
      (entry) => entry.extId === location.target && gmailIdOf(entry) !== undefined,
    )
  )
    throw new Error();
  if (!location.id) return { location };
  const detail = await fetchClient.GET("/v1/drafts/{id}", {
    params: { path: { id: location.id }, query: { accountId: location.account } },
    cache: "no-store",
  });
  const draft = detail.data;
  if (
    !draft ||
    !validatedDraft(draft, location.account, location.id) ||
    draft.kind !== "reply" ||
    draft.rootId !== location.root ||
    draft.replyToEntry !== location.target ||
    !draft.messageId ||
    (!fresh && draft.messageId !== location.messageId)
  )
    throw new Error();
  const check = await fetchClient.GET("/v1/drafts/{id}/save-check", {
    params: {
      path: { id: location.id },
      query: { accountId: location.account, expectedMessageId: draft.messageId },
    },
    cache: "no-store",
  });
  if (
    check.data?.status !== "supported" ||
    check.data.context !== "reply" ||
    !check.data.unchangedCompatible ||
    !check.data.bodyEditCompatible
  )
    throw new Error();
  return { location: { ...location, messageId: draft.messageId }, draft };
}

/** One initiating app tree, not a lock across independent tabs or browser restarts. */
export default function useThreadPopouts({
  editor,
  incoming,
  canRestore,
  restore,
}: {
  editor: MutableRefObject<(() => LeaveSnapshot) | undefined>;
  incoming?: { token: string; root: string; account: string; id?: string };
  canRestore: (location: PopoutLocation) => boolean;
  restore: (location: PopoutLocation) => Promise<void>;
}): ThreadPopouts & { moving: MutableRefObject<boolean> } {
  const [, update] = useState(0);
  const [frozen, setFrozen] = useState(false);
  const [active, setActive] = useState(false);
  const [prepared, setPrepared] = useState<PreparedPopout>();
  const [message, setMessage] = useState("");
  // A managed window cannot become standalone by dropping mutable URL flags.
  const managed = useRef(incoming);
  if (!managed.current && incoming) managed.current = incoming;
  const route = managed.current;
  const reader = useRef<PopoutLocation>();
  const moving = useRef(false);
  const away = useRef(new Map<string, Away>());
  const pending = useRef<(() => void) | undefined>();
  const child = useRef<{
    channel: BroadcastChannel;
    location: PopoutLocation;
    returning?: ReturnType<typeof witness>;
    released: boolean;
    attempt?: string;
  }>();
  const latest = useRef({ restore, canRestore });
  latest.current = { restore, canRestore };
  const wake = () => update((n) => n + 1);
  const childTimer = useRef<number>();
  const relinquished = useRef(false);
  const cancelIncoming = useRef<(() => void) | undefined>();

  const witness = useCallback(
    (root: string, account?: string) => {
      const get = editor.current;
      const snapshot = get?.();
      if (get && snapshot) {
        const proof = snapshot.popout;
        if (
          snapshot.busy ||
          snapshot.kind !== "reply" ||
          snapshot.destination !== root ||
          !proof ||
          (account && proof.accountId !== account)
        )
          return;
        return {
          get,
          revision: snapshot.revision,
          invalidate: snapshot.invalidate,
          location: {
            root,
            account: proof.accountId,
            mode: "reply" as const,
            id: proof.id,
            messageId: proof.messageId,
            target: proof.target,
          },
        };
      }
      const proof = reader.current;
      if (proof?.root === root && proof.account === (account ?? ""))
        return {
          get: undefined,
          revision: JSON.stringify(proof),
          invalidate: () => {},
          location: proof,
        };
    },
    [editor],
  );
  const stillCurrent = useCallback(
    (proof: NonNullable<ReturnType<typeof witness>>) => {
      const current = witness(proof.location.root, proof.location.account);
      return (
        current &&
        current.get === proof.get &&
        current.revision === proof.revision &&
        same(current.location, proof.location)
      );
    },
    [witness],
  );
  function install(preparation: PreparedPopout) {
    flushSync(() => setPrepared(preparation));
  }
  async function admit(entry: Away, token: string, preparation: PreparedPopout) {
    if (
      away.current.get(token) !== entry ||
      entry.admitting ||
      !latest.current.canRestore(preparation.location)
    )
      throw new Error();
    entry.admitting = true;
    try {
      install(preparation);
      await latest.current.restore(preparation.location);
      away.current.delete(token);
      flushSync(wake);
    } finally {
      entry.admitting = false;
    }
  }
  const find = (root: string, account?: string) =>
    [...away.current.entries()].find(
      ([, entry]) =>
        entry.location.root === root &&
        (entry.location.mode === "reader"
          ? entry.location.account === (account ?? "")
          : !account || entry.location.account === account),
    );

  useEffect(
    () => () => {
      pending.current?.();
      for (const item of away.current.values()) item.channel.close();
      away.current.clear();
    },
    [],
  );

  useEffect(() => {
    if (!route) return;
    setActive(false);
    if (relinquished.current) return;
    setMessage("Preparing this window…");
    if (!/^[a-f0-9-]{36}$/.test(route.token) || !route.root || !globalThis.BroadcastChannel) {
      setMessage("This move could not be verified. Return to the original window.");
      return;
    }
    const channel = new BroadcastChannel(channelName(route.token));
    let alive = true;
    let offered: PopoutLocation | undefined;
    let preparation: PreparedPopout | undefined;
    let committed = false;
    const timer = window.setTimeout(() => {
      if (!alive || committed) return;
      channel.postMessage({ type: "cancel" });
      setMessage("The move was not completed. Keep using the original window.");
      channel.close();
      alive = false;
    }, WAIT);
    const cancel = () => {
      if (!alive) return;
      alive = false;
      window.clearTimeout(timer);
      window.clearTimeout(childTimer.current);
      channel.postMessage({ type: "cancel" });
      channel.close();
    };
    cancelIncoming.current = cancel;
    channel.onmessage = async ({ data }: MessageEvent<Packet>) => {
      if (!alive || relinquished.current || !data || typeof data !== "object") return;
      if (data.type === "offer" && !offered) {
        const location = data.location;
        if (
          !validLocation(location) ||
          location.root !== route.root ||
          location.account !== route.account ||
          location.id !== route.id
        )
          return;
        offered = location;
        try {
          preparation = await prepare(location);
          if (alive && !relinquished.current) channel.postMessage({ type: "ready", location });
        } catch {
          if (alive) {
            channel.postMessage({ type: "cancel" });
            setMessage("The thread or draft could not be checked. Keep using the original window.");
          }
        }
      } else if (
        data.type === "commit" &&
        preparation &&
        same(data.location, offered) &&
        !committed
      ) {
        committed = true;
        window.clearTimeout(timer);
        child.current = { channel, location: offered!, released: false };
        setPrepared(preparation);
        setMessage("");
        setActive(true);
        channel.postMessage({ type: "started", location: offered });
      } else if (data.type === "cancel" && !committed) {
        alive = false;
        window.clearTimeout(timer);
        setMessage("The move was cancelled. Keep using the original window.");
        channel.close();
      } else if (data.type === "focus" && committed) window.focus();
      else if (
        data.type === "return-refused" &&
        child.current?.returning &&
        !child.current.released &&
        data.attempt === child.current.attempt
      ) {
        window.clearTimeout(childTimer.current);
        child.current.returning = undefined;
        moving.current = false;
        setFrozen(false);
        setMessage(
          "The main window cannot accept this thread yet. Close its other editor, then try Return again. Your input stays here.",
        );
      } else if (
        data.type === "return-ready" &&
        child.current?.returning &&
        !child.current.released &&
        data.attempt === child.current.attempt &&
        same(data.location, child.current.returning.location)
      ) {
        const proof = child.current.returning;
        window.clearTimeout(childTimer.current);
        if (!stillCurrent(proof)) {
          child.current.returning = undefined;
          moving.current = false;
          setFrozen(false);
          channel.postMessage({ type: "return-cancel", attempt: child.current.attempt });
          setMessage("The reply changed. Finish saving before returning.");
          return;
        }
        child.current.released = true;
        proof.invalidate();
        flushSync(() => setActive(false));
        setMessage("Returning… Keep this window paused until the main window confirms.");
        channel.postMessage({
          type: "return-commit",
          location: proof.location,
          attempt: child.current.attempt,
        });
      } else if (data.type === "returned" && committed && child.current?.released) {
        window.close();
        setMessage("Returned to the main window. You can close this window.");
      }
    };
    channel.postMessage({ type: "hello" });
    return () => {
      alive = false;
      window.clearTimeout(timer);
      window.clearTimeout(childTimer.current);
      channel.close();
      child.current = undefined;
      if (cancelIncoming.current === cancel) cancelIncoming.current = undefined;
    };
    // The immutable managed identity survives ordinary same-scope draft selection.
  }, [route, stillCurrent]);

  useEffect(() => {
    if (!route) return;
    const release = () => {
      relinquished.current = true;
      const session = child.current;
      if (!session) {
        cancelIncoming.current?.();
        flushSync(() => {
          setActive(false);
          setFrozen(false);
        });
        setMessage("This move was abandoned. Keep using the original window.");
        return;
      }
      if (session.released) return;
      const proof = witness(session.location.root, session.location.account);
      session.released = true;
      moving.current = true;
      proof?.invalidate();
      flushSync(() => {
        setActive(false);
        setFrozen(false);
      });
      setMessage(
        "This window relinquished its thread. Reopen saved state in the main window; unsaved input was not returned.",
      );
      // pagehide happens only after a non-cancelled leave. A BFCache restore stays inert.
      session.channel.postMessage({
        type: proof ? "native-release" : "native-uncertain",
        location: proof?.location ?? session.location,
      });
    };
    window.addEventListener("pagehide", release);
    return () => window.removeEventListener("pagehide", release);
  });

  function start(root: string, account?: string) {
    if (moving.current || route || find(root, account)) return;
    const proof = witness(root, account);
    if (!proof || !validLocation(proof.location)) {
      setMessage(
        "Finish saving this reply and restore default reply options before moving it. Unsaved input, review, and stopped saves stay here.",
      );
      return;
    }
    if (!globalThis.BroadcastChannel || !globalThis.crypto?.randomUUID) {
      setMessage("This browser cannot safely move the thread to another window.");
      return;
    }
    const token = crypto.randomUUID();
    const channel = new BroadcastChannel(channelName(token));
    const entry: Away = { location: proof.location, channel, committed: false };
    let released = false;
    let cancelled = false;
    moving.current = true;
    setFrozen(true);
    setMessage("");
    const cancel = () => {
      if (released || cancelled) return;
      cancelled = true;
      channel.postMessage({ type: "cancel" });
      channel.close();
      window.clearTimeout(timer);
      pending.current = undefined;
      moving.current = false;
      setFrozen(false);
    };
    const timer = window.setTimeout(() => {
      cancel();
      if (!released)
        setMessage(
          "No prepared popup replied. Your thread remains here; allow popups and try again.",
        );
    }, WAIT);
    pending.current = cancel;
    channel.onmessage = async ({ data }: MessageEvent<Packet>) => {
      if (cancelled || !data || typeof data !== "object") return;
      if (data.type === "hello" && !released)
        channel.postMessage({ type: "offer", location: proof.location });
      else if (data.type === "cancel" && !released) cancel();
      else if (data.type === "ready" && !released && same(data.location, proof.location)) {
        if (!stillCurrent(proof)) {
          cancel();
          setMessage(
            "The reply changed while opening the popup. It remains here; try again after saving.",
          );
          return;
        }
        released = true;
        window.clearTimeout(timer);
        pending.current = undefined;
        proof.invalidate();
        away.current.set(token, entry);
        flushSync(() => {
          setFrozen(false);
          wake();
        });
        moving.current = false;
        channel.postMessage({ type: "commit", location: proof.location });
      } else if (data.type === "started" && released && same(data.location, proof.location))
        entry.committed = true;
      else if (
        (data.type === "return-prepare" || data.type === "native-release") &&
        released &&
        entry.committed &&
        validLocation(data.location) &&
        data.location.root === root &&
        data.location.account === proof.location.account
      ) {
        const location = data.location;
        if (data.type === "return-prepare" && typeof data.attempt !== "string") return;
        if (data.type === "native-release") {
          entry.location = location;
          entry.returning = undefined;
          moving.current = false;
        }
        entry.attempt = data.attempt;
        try {
          if (!latest.current.canRestore(location)) throw new Error();
          const preparedReturn = await prepare(location);
          if (
            !latest.current.canRestore(location) ||
            away.current.get(token) !== entry ||
            entry.attempt !== data.attempt
          )
            throw new Error();
          if (data.type === "native-release") {
            entry.committed = false;
            await admit(entry, token, preparedReturn);
            channel.postMessage({ type: "returned" });
            channel.close();
          } else {
            entry.returning = preparedReturn;
            moving.current = true;
            channel.postMessage({ type: "return-ready", location, attempt: data.attempt });
          }
        } catch {
          channel.postMessage({ type: "return-refused", attempt: data.attempt });
          setMessage(
            "The thread remains in its own window. Close other editors before returning; if it is closed, use Reopen saved state.",
          );
        }
      } else if (
        data.type === "return-cancel" &&
        released &&
        entry.committed &&
        data.attempt === entry.attempt
      ) {
        entry.returning = undefined;
        entry.attempt = undefined;
        moving.current = false;
      } else if (
        data.type === "return-commit" &&
        entry.returning &&
        data.attempt === entry.attempt &&
        same(data.location, entry.returning.location)
      ) {
        entry.location = entry.returning.location;
        entry.committed = false;
        try {
          await admit(entry, token, entry.returning);
          channel.postMessage({ type: "returned" });
          channel.close();
        } catch {
          setMessage(
            "Return was interrupted after release. Close the paused popup, then use Reopen saved state.",
          );
        } finally {
          moving.current = false;
        }
      } else if (data.type === "native-uncertain" && released && entry.committed) {
        entry.returning = undefined;
        entry.attempt = undefined;
        moving.current = false;
        setMessage(
          "The popup closed without an acknowledged return. Unsaved input was not recovered. Confirm it is closed, then use Reopen saved state.",
        );
      }
    };
    const url = new URL("/", window.location.origin);
    url.searchParams.set("open", root);
    url.searchParams.set("accountId", proof.location.account);
    url.searchParams.set("popup", "1");
    url.searchParams.set("handoff", token);
    if (proof.location.id) url.searchParams.set("draft", proof.location.id);
    try {
      window.open(
        url.href,
        "_blank",
        "popup=yes,width=900,height=800,resizable=yes,scrollbars=yes,noopener",
      );
    } catch {
      cancel();
      setMessage("The popup could not be opened. Your thread remains here.");
    }
  }
  function returnToMain() {
    const session = child.current;
    if (!session || session.released || moving.current) return;
    const proof = witness(session.location.root, session.location.account);
    if (!proof) {
      setMessage(
        "Finish saving and restore default reply options before returning. Your input stays in this window.",
      );
      return;
    }
    moving.current = true;
    setFrozen(true);
    session.returning = proof;
    session.attempt = crypto.randomUUID();
    setMessage("Checking the main window…");
    // Nothing has been released yet, so a lost preparation reply can safely keep the child.
    childTimer.current = window.setTimeout(() => {
      if (session.released) return;
      session.returning = undefined;
      moving.current = false;
      setFrozen(false);
      session.channel.postMessage({ type: "return-cancel", attempt: session.attempt });
      setMessage("The main window did not confirm. Your thread remains here; try again.");
    }, WAIT);
    session.channel.postMessage({
      type: "return-prepare",
      location: proof.location,
      attempt: session.attempt,
    });
  }
  async function recover(root: string, account?: string) {
    const found = find(root, account);
    if (
      !found ||
      moving.current ||
      !window.confirm(
        "Only reopen after the other thread window is really closed. Unsaved input and uncertain saves cannot be recovered here. Reopen fresh saved state?",
      )
    )
      return;
    const [token, entry] = found;
    moving.current = true;
    try {
      if (!latest.current.canRestore(entry.location)) throw new Error();
      const preparation = await prepare(entry.location, true);
      await admit(entry, token, preparation);
      entry.channel.close();
      setMessage("Reopened fresh saved state. Unsaved popup input was not recovered.");
    } catch {
      setMessage(
        "Saved state could not be checked, or another editor is open. The thread remains paused; close other editors and retry.",
      );
    } finally {
      moving.current = false;
    }
  }
  return {
    moving,
    frozen,
    hidden: Boolean(route && !active),
    managed: Boolean(route),
    scope: route,
    reader,
    prepared,
    initialTarget: prepared?.location,
    message,
    away: (root, account) => Boolean(find(root, account)),
    start,
    focus: (root, account) => find(root, account)?.[1].channel.postMessage({ type: "focus" }),
    recover: (root, account) => {
      void recover(root, account);
    },
    returnToMain: route && active ? returnToMain : undefined,
  };
}
