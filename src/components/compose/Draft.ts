import { typedAddress, type Address } from "./AddressField";

export type Draft = { to: Address[]; cc: Address[]; subject: string; body: string };

export const emptyDraft: Draft = { to: [], cc: [], subject: "", body: "" };

export function parseAddressHeader(header?: string): Address[] {
  if (!header) return [];
  const parts: string[] = [];
  let current = "";
  let angle = 0;
  let quoted = false;
  for (const char of header) {
    if (char === '"') quoted = !quoted;
    if (!quoted && char === "<") angle++;
    if (!quoted && char === ">") angle--;
    if (char === "," && !quoted && angle === 0) {
      parts.push(current.trim());
      current = "";
    } else current += char;
  }
  if (current.trim()) parts.push(current.trim());
  return parts.flatMap((part) => {
    const address = typedAddress(part);
    return address ? [address] : [];
  });
}
