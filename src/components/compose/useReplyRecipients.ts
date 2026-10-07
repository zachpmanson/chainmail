import { useEffect, useMemo, useState } from "react";
import { $api, type CorpusEntry, type SendResponse } from "../../lib/api/api";
import { addressesOf, usePersonAddresses } from "../../lib/message/who";
import { addressKey, type Address } from "./AddressField";

/**
 * A reply's To and Cc. Each list has its own touched bit: an untouched list is left
 * out of requests so the mailbox's default (notably Reply-To) stays authoritative.
 * Starts over when `answer` or `resetKey` changes.
 */
export default function useReplyRecipients(answer: CorpusEntry, all: boolean, resetKey: unknown) {
  const [to, setTo] = useState<Address[]>([]);
  const [cc, setCc] = useState<Address[]>([]);
  const [toTouched, setToTouched] = useState(false);
  const [ccTouched, setCcTouched] = useState(false);

  const settings = $api.useQuery("get", "/v1/settings", {});
  const people = usePersonAddresses();
  const roster = $api.useQuery("get", "/v1/people", {});

  // The reader's own addresses, which must not appear in the suggested audience.
  const mine = useMemo(
    () =>
      settings.data?.mePersonId === undefined ? [] : (people.get(settings.data.mePersonId) ?? []),
    [people, settings.data],
  );

  // Seeded from the message's actual headers, not the identity graph: expanding a
  // recipient to every known alias could send to addresses the message did not include.
  const defaults = useMemo(() => {
    const seen = new Set(mine.map(addressKey));
    const to: Address[] = [];
    const cc: Address[] = [];
    const sender = answer.fromEmail ? addressKey(answer.fromEmail) : "";
    const offer = (list: Address[], who: Address) => {
      const key = addressKey(who.address);
      if (!key || seen.has(key) || key === sender) return;
      seen.add(key);
      list.push(who);
    };
    if (answer.fromEmail) {
      const key = addressKey(answer.fromEmail);
      if (!seen.has(key)) {
        seen.add(key);
        to.push({ name: answer.author ?? undefined, address: answer.fromEmail });
      }
    }
    for (const who of [...(answer.toRecipients ?? []), ...(answer.ccRecipients ?? [])]) {
      offer(cc, { name: who.name, address: who.address });
    }
    return { to, cc };
  }, [answer, mine]);

  // Declared before the defaults effect so a reset is reseeded in the same commit.
  useEffect(() => {
    setTo([]);
    setCc([]);
    setToTouched(false);
    setCcTouched(false);
  }, [answer.extId, resetKey]);

  // Once a list is edited, it belongs to the reader.
  useEffect(() => {
    if (!toTouched) setTo(defaults.to);
    if (!ccTouched) setCc(all ? defaults.cc : []);
  }, [all, ccTouched, defaults, toTouched]);

  // Addresses on this message first, then everybody else the corpus knows.
  const suggestions = useMemo(() => {
    const out: Address[] = [];
    const seen = new Set<string>();
    const offer = (who: Address) => {
      const key = addressKey(who.address);
      if (!key || seen.has(key)) return;
      seen.add(key);
      out.push(who);
    };
    if (answer.fromEmail) offer({ name: answer.author ?? undefined, address: answer.fromEmail });
    for (const who of [...(answer.toRecipients ?? []), ...(answer.ccRecipients ?? [])])
      offer({ name: who.name, address: who.address });
    for (const p of answer.participants ?? [])
      for (const address of people.get(p.personId) ?? []) offer({ name: p.name, address });
    for (const p of roster.data?.people ?? [])
      for (const address of addressesOf(p.identities)) offer({ name: p.displayName, address });
    return out;
  }, [answer, people, roster.data]);

  return {
    to,
    cc,
    toTouched,
    mine,
    suggestions,
    // Edited lists go as bare addresses; the mailbox keeps names the message already carried.
    edited: {
      ...(toTouched ? { to: to.map((a) => a.address) } : {}),
      ...(ccTouched ? { cc: cc.map((a) => a.address) } : {}),
    },
    changeTo: (next: Address[]) => {
      setToTouched(true);
      setTo(next);
    },
    changeCc: (next: Address[]) => {
      setCcTouched(true);
      setCc(next);
    },
    /** Replace the corpus's estimates with the exact addresses the mailbox will use. */
    adopt: (plan: SendResponse) => {
      if (plan.toRecipients) setTo(plan.toRecipients);
      if (plan.ccRecipients) setCc(plan.ccRecipients);
    },
    untouch: () => {
      setToTouched(false);
      setCcTouched(false);
    },
  };
}
