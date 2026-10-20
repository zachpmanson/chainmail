/** Decode before any numeric coercion: ids and search text can exceed JS precision. */
export function parseTextSearch(search: string): Record<string, unknown> {
  const result: Record<string, unknown> = Object.create(null);
  for (const [key, raw] of new URLSearchParams(search)) {
    let value = raw;
    // Existing router-generated links JSON-quote strings that look like JSON.
    if (raw.startsWith('"')) {
      try {
        const decoded: unknown = JSON.parse(raw);
        if (typeof decoded === "string") value = decoded;
      } catch {
        // Ordinary search text need not be valid JSON.
      }
    }
    const previous = result[key];
    result[key] =
      previous === undefined
        ? value
        : Array.isArray(previous)
          ? [...previous, value]
          : [previous, value];
  }
  return result;
}
