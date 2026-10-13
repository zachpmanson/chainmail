/** Score only markup signals that distinguish a designed email from correspondence. */
export function originalHtmlScore(html: string): number {
  const tables = (html.match(/<table\b/gi) ?? []).length;
  const links = (html.match(/<a\b/gi) ?? []).length;
  const images = (html.match(/<img\b/gi) ?? []).length;
  const styledElements = (html.match(/<(?:[a-z][\w:-]*)\b[^>]*(?:\bstyle\s*=|\bclass\s*=)/gi) ?? [])
    .length;
  return (
    (tables >= 2 ? 2 : 0) + (styledElements >= 3 ? 1 : 0) + (images >= 1 && links >= 2 ? 1 : 0)
  );
}

export function defaultsToOriginal(html: string): boolean {
  return html.trim() !== "" && originalHtmlScore(html) >= 3;
}
