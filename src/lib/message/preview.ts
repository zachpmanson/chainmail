/** A PDF frame must not reinterpret a response labelled by another attachment sharing its digest. */
export function isPDFPreview(type: string, prefix: Uint8Array): boolean {
  return (
    type.split(";")[0]!.trim().toLowerCase() === "application/pdf" &&
    [0x25, 0x50, 0x44, 0x46, 0x2d].every((byte, i) => prefix[i] === byte)
  );
}
