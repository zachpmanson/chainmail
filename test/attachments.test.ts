import { describe, expect, it } from "vitest";
import {
  attHref,
  hasPreview,
  isPicture,
  pullSummary,
  skipNote,
  thumbnail,
  type Attachment,
} from "../src/lib/message/attachments";

const file = (extra: Partial<Attachment> = {}): Attachment => ({ name: "f.png", ...extra });

describe("attHref", () => {
  it("prefers local bytes, then Gmail, then the link", () => {
    const all = file({ blobSha: "abc", gmailId: "g1", link: "https://slack.example/f" });
    expect(attHref(all, "/v1/attachments")).toBe("/v1/attachments/abc");
    expect(attHref(all)).toBe("https://mail.google.com/mail/#all/g1");
    expect(attHref(file({ link: "https://slack.example/f" }))).toBe("https://slack.example/f");
  });

  it("is undefined for a quote-recovered file", () => {
    expect(attHref(file({ link: "" }), "/v1/attachments")).toBeUndefined();
  });
});

describe("pictures and thumbnails", () => {
  it("accepts only data:image previews", () => {
    expect(hasPreview(file({ preview: "data:image/png;base64,AA" }))).toBe(true);
    expect(hasPreview(file({ preview: "https://x/p.png" }))).toBe(false);
  });

  it("reads view, falling back to kind", () => {
    expect(isPicture(file({ view: "image" }))).toBe(true);
    expect(isPicture(file({ kind: "Image" }))).toBe(true);
    expect(isPicture(file({ view: "pdf", kind: "pdf" }))).toBe(false);
  });

  it("uses the embedded preview, then pulled bytes of a picture", () => {
    expect(
      thumbnail(
        file({ preview: "data:image/png;base64,AA", previewW: 40, previewH: 30, blobSha: "abc" }),
        "/m",
      ),
    ).toEqual({ src: "data:image/png;base64,AA", blob: false, w: 40, h: 30 });
    expect(thumbnail(file({ blobSha: "abc", view: "image" }), "/m")).toEqual({
      src: "/m/abc",
      blob: true,
    });
    expect(thumbnail(file({ blobSha: "abc", view: "pdf" }), "/m")).toBeUndefined();
    expect(thumbnail(file({ blobSha: "abc", view: "image" }))).toBeUndefined();
  });
});

describe("skipNote", () => {
  it("explains known reasons and passes unknown ones through", () => {
    expect(skipNote(file())).toBeUndefined();
    expect(skipNote(file({ skip: "too_large" }))).toBe("not stored: larger than the fetch cap");
    expect(skipNote(file({ skip: "mystery" }))).toBe("not stored (mystery)");
  });
});

describe("pullSummary", () => {
  const pull = { wanted: 3, pulled: 1, skipped: 1, failed: 1, bytes: 10 };

  it("says when nothing needed fetching", () => {
    expect(pullSummary({ ...pull, wanted: 0, files: [] })).toBe("no files needed fetching");
  });

  it("counts fetched, declined and failed files", () => {
    expect(
      pullSummary({
        ...pull,
        files: [
          { name: "a.pdf" },
          { name: "b.zip", reason: "too large" },
          { name: "c.png", error: "timeout" },
        ],
      }),
    ).toBe(
      "1/3 files fetched, 1 declined (b.zip: too large), 1 failed, will retry (c.png: timeout)",
    );
  });
});
