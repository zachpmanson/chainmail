import { describe, expect, it } from "vitest";
import { isPDFPreview } from "../src/lib/message/preview";

const bytes = (text: string) => new TextEncoder().encode(text);

describe("PDF preview boundary", () => {
  it("requires both a PDF response type and a PDF signature", () => {
    expect(isPDFPreview("application/pdf", bytes("%PDF-1.7\nfixture"))).toBe(true);
    expect(isPDFPreview("Application/PDF; charset=binary", bytes("%PDF-1.7"))).toBe(true);
    for (const type of ["text/html", "image/svg+xml", "application/octet-stream", ""]) {
      expect(isPDFPreview(type, bytes("%PDF-1.7\nfixture"))).toBe(false);
      expect(isPDFPreview(type, bytes("inert marker"))).toBe(false);
    }
    expect(isPDFPreview("application/pdf", bytes("inert marker"))).toBe(false);
    expect(isPDFPreview("application/pdf", bytes("%PDF"))).toBe(false);
  });
});
