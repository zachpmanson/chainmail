export function errText(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}
