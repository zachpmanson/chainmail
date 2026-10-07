import { useMemo } from "react";
import { $api, type CorpusEntry } from "../api/api";

/** Email identities only; the corpus also keeps Slack uids and display names. */
export function addressesOf(identities: readonly string[] | undefined): string[] {
  const out: string[] = [];
  for (const identity of identities ?? []) {
    if (!identity.startsWith("email:")) continue;
    const address = identity.slice("email:".length);
    if (address && !out.includes(address)) out.push(address);
  }
  return out;
}

export function withAddress(name: string, addresses: readonly string[] | undefined): string {
  return addresses?.length ? `${name} <${addresses.join(", ")}>` : name;
}

/**
 * Splits a receipt's `to:` line, whose format is set by recipientLine in
 * internal/spec/people.go. A display name containing ", " gets split too.
 */
export function receiptNames(to: string): { text: string; name: string }[] {
  return to
    .split(", ")
    .map((text) => text.trim())
    .filter((text) => text !== "")
    .map((text) => ({ text, name: text.replace(/^(cc|to|bcc)\s+/i, "") }));
}

/** Matches derive.ts's whoTitle. A quoted-only message has no From header, and an
 *  address matched by name alone isn't evidence, so the quoter is named instead. */
export function senderTitle(e: CorpusEntry): string {
  const name = e.author ?? "";
  if (!e.fromEmail) {
    if (!e.fromQuotedBy) return name;
    const unknown = `address unknown; quoted by ${e.fromQuotedBy}`;
    return name ? `${name} (${unknown})` : unknown;
  }
  if (!name) return e.fromEmail;
  return `${name} <${e.fromEmail}>`;
}

export function usePersonAddresses(): Map<number, string[]> {
  const answer = $api.useQuery("get", "/v1/people", {});
  return useMemo(() => {
    const out = new Map<number, string[]>();
    for (const person of answer.data?.people ?? []) {
      const addresses = addressesOf(person.identities);
      if (addresses.length) out.set(person.personId, addresses);
    }
    return out;
  }, [answer.data]);
}
