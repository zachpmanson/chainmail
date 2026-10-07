import type { ComposeResponse } from "../../lib/api/api";
import useAccounts from "./useAccounts";

export default function ComposePreview({ preview }: { preview: ComposeResponse }) {
  const { nameOf } = useAccounts();
  return (
    <dl className="grid grid-cols-[5rem_minmax(0,1fr)] gap-2 wrap-anywhere">
      <dt className="font-bold">Sending account</dt>
      <dd className="m-0">{nameOf(preview.accountId)}</dd>
      <dt className="font-bold">To</dt>
      <dd className="m-0">{preview.to}</dd>
      {preview.cc ? (
        <>
          <dt className="font-bold">Cc</dt>
          <dd className="m-0">{preview.cc}</dd>
        </>
      ) : null}
      <dt className="font-bold">Subject</dt>
      <dd className="m-0">{preview.subject}</dd>
      <dt className="font-bold">Plain-text message</dt>
      <dd className="m-0">
        <pre className="m-0 whitespace-pre-wrap wrap-anywhere [font:inherit]">{preview.body}</pre>
      </dd>
      <p className="col-span-full">Review the exact message above. Sending is irreversible.</p>
    </dl>
  );
}
