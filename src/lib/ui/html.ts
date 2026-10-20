/** Plain text inserted into an HTML template, never trusted markup. */
export function escapeHtmlText(text: string): string {
  return text.replace(
    /[&<>"']/g,
    (character) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[character]!,
  );
}

/** The dangerouslySetInnerHTML shape. */
export const html = (s: string) => ({ __html: s });
