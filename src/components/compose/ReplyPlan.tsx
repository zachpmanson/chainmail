import type { SendResponse } from "../../lib/api/api";
import useAccounts from "./useAccounts";

export default function ReplyPlan({ plan }: { plan: SendResponse }) {
  const { nameOf } = useAccounts();
  return (
    <div className="text-xs leading-normal">
      <p className="m-0 mb-2">
        <strong>Nothing has been sent yet.</strong> This is the whole message as it will go from{" "}
        <strong>{nameOf(plan.accountId)}</strong>: to <strong>{plan.to || "(no recipient)"}</strong>
        {plan.cc ? (
          <>
            , cc <strong>{plan.cc}</strong>
          </>
        ) : null}
        , as <strong>{plan.subject}</strong>, in{" "}
        <strong>{plan.html ? "text and HTML" : "plain text alone"}</strong>. Your words come first
        and the message you are answering is quoted under them.
      </p>
      {plan.html ? (
        <div
          className="replyhtml mt-1.5 max-h-88 overflow-auto wrap-anywhere rounded-md border border-line bg-bg p-2 text-sm leading-normal text-fg"
          dangerouslySetInnerHTML={{ __html: plan.html }}
        />
      ) : (
        <pre className="mt-1.5 max-h-88 overflow-auto wrap-anywhere whitespace-pre-wrap rounded-md border border-line bg-bg p-2 font-mono text-xs leading-normal text-fg">
          {plan.body}
        </pre>
      )}
    </div>
  );
}
