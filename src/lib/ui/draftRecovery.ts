export type RecoveryDraft = { to: string[]; cc: string[]; subject: string; body: string };

/** Select only editable content, keeping pasted header newlines out of the recovery headers. */
export function draftRecoveryText({ to, cc, subject, body }: RecoveryDraft): string {
  const header = (value: string) => value.replace(/[\r\n]+/gu, " ");
  return `To: ${to.map(header).join(", ")}\nCc: ${cc.map(header).join(", ")}\nSubject: ${header(subject)}\n\n${body}`;
}
