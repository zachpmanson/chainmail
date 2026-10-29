import { describe, expect, it } from "vitest";
import cases from "../testdata/quotes/replaced-links.json";
import { diffBaseToEdit, editHtml } from "../src/lib/timeline/editDiff";
import { hoistEdits } from "../src/lib/thread/hoist";
import { corpus } from "./fixtures";

const bare = (html: string) => html.replace(/ class="[^"]*"/g, "");

describe("anonymised replaced quote links", () => {
  for (const tc of cases) {
    it(`${tc.name}: highlights the changed destination and hoists the duplicate`, () => {
      const oldURL = tc.original.match(/https?:\/\/\S+/)![0];
      const newURL = tc.quoted.match(/https?:\/\/\S+/)![0];
      const diff = diffBaseToEdit(tc.original, tc.quoted);
      expect(diff.filter((s) => s.kind === "del").map((s) => s.text)).toEqual([oldURL]);
      expect(diff.filter((s) => s.kind === "ins").map((s) => s.text)).toEqual([newURL]);
      // The real recovered copy is text-only; the API supplies its sanitised HTML.
      const formatted = `<p>${tc.quoted.replace(/\n/g, "<br>")}</p>`;
      const original = `<p>${tc.original.replace(/\r?\n/g, "<br>")}</p>`;
      const html = bare(editHtml(formatted, original, tc.quoted));
      expect(html).toContain(`<b>${newURL}</b>`);
      expect(html).not.toContain(oldURL);
      const { shown } = hoistEdits([
        corpus("original", { body: tc.original }),
        corpus("copy", { parent: "original", body: tc.quoted }),
        corpus("host", {
          parent: "original",
          edits: [{ id: "copy", base: "original", body: tc.quoted }],
        }),
      ]);
      expect(shown.map((entry) => entry.extId)).toEqual(["original", "host"]);
      expect(shown[1]!.edits![0]!.body).toBe(tc.quoted);
    });
  }
});
