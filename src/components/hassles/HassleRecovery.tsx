import useNativeOperations from "../../lib/hassles/useNativeOperations";
import { ready } from "../../lib/hassles/native";
import Button from "../ui/Button";
import InlineAlert from "../ui/InlineAlert";

export default function HassleRecovery() {
  const { store, state, mutations } = useNativeOperations();
  const recovery = state.recovery;
  if (ready(recovery))
    return state.error ? (
      <InlineAlert>
        {state.error}{" "}
        {state.request
          ? "New reminder operations are blocked until you explicitly review the retained original or start a separate creation."
          : ""}
      </InlineAlert>
    ) : null;
  const pending = recovery?.pending;
  return (
    <section
      aria-label="Reminder operation status"
      className="min-w-0 rounded-md border border-line p-4 wrap-anywhere"
    >
      {recovery?.status === "pending" && pending ? (
        <>
          <p>
            Pending {pending.action}: {pending.title} · {pending.components} components ·{" "}
            {pending.location.accountId} · {pending.location.calendarId}. All reminder changes and
            calendar selection are blocked.
          </p>
          <Button
            disabled={mutations > 0 || !pending.operationId}
            onClick={() => void store.refreshOperation()}
          >
            Refresh operation
          </Button>
          <p className="text-sm text-muted">
            Read-only reconciliation of the original operation. No reminders will be resent.
          </p>
        </>
      ) : (
        <p>
          {recovery?.status === "busy"
            ? "Reminder store is busy. All reminder changes are blocked."
            : "Reminder storage is unavailable. The operator must enable approved storage before changes are allowed."}
        </p>
      )}
      {state.error ? <InlineAlert>{state.error}</InlineAlert> : null}
    </section>
  );
}
