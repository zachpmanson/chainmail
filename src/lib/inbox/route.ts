/** A page title, reduced to what POST /v1/spec will accept as a name. */
export function slug(title: string): string {
  return (
    title
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "")
      .slice(0, 64) || ""
  );
}

/** Clock-based so two untitled builds never overwrite each other. */
export function untitledName(): string {
  return `spec-${Date.now().toString(36)}`;
}
