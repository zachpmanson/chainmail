import type { Address } from "./AddressField";

export type Draft = { to: Address[]; cc: Address[]; subject: string; body: string };

export const emptyDraft: Draft = { to: [], cc: [], subject: "", body: "" };
