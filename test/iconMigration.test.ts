import { IconChevronLeft, IconChevronRight } from "@tabler/icons-react";
import { readdirSync, readFileSync } from "node:fs";
import { join, relative } from "node:path";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import ts from "typescript";
import { describe, expect, it } from "vitest";
import AttachmentCount from "../src/components/inbox/AttachmentCount";
import MailCount from "../src/components/inbox/MailCount";
import PeopleCount from "../src/components/inbox/PeopleCount";
import ReplyTarget from "../src/components/compose/ReplyTarget";
import Attachments from "../src/components/thread/Attachments";
import ReplyLink from "../src/components/thread/ReplyLink";
import MergeConfirm from "../src/components/ops/MergeConfirm";

function sourceFiles(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const path = join(dir, entry.name);
    return entry.isDirectory() ? sourceFiles(path) : /\.tsx?$/.test(path) ? [path] : [];
  });
}

describe("app-wide interface icon migration", () => {
  it("uses only Tabler icon imports and leaves handwritten SVG only in the data minimap", () => {
    const violations: string[] = [];
    const icons = new Set<string>();
    for (const path of sourceFiles("src")) {
      const file = ts.createSourceFile(
        path,
        readFileSync(path, "utf8"),
        ts.ScriptTarget.Latest,
        true,
      );
      function visit(node: ts.Node) {
        if (ts.isImportDeclaration(node) && ts.isStringLiteral(node.moduleSpecifier)) {
          const library = node.moduleSpecifier.text;
          if (!library.startsWith(".") && /icons|lucide|phosphor|fontawesome/i.test(library)) {
            if (library !== "@tabler/icons-react") violations.push(`${path}: ${library}`);
            const bindings = node.importClause?.namedBindings;
            if (bindings && ts.isNamedImports(bindings)) {
              for (const icon of bindings.elements) icons.add(icon.name.text);
            }
          }
        }
        if (ts.isJsxOpeningElement(node) || ts.isJsxSelfClosingElement(node)) {
          const tag = node.tagName.getText(file);
          // These SVGs encode reply-tree data and its matching legend, not interface icons.
          if (tag === "svg" && relative("src", path) !== "components/specs/Minimap.tsx") {
            violations.push(`${path}: handwritten interface SVG`);
          }
          if (tag.startsWith("Icon") && icons.has(tag)) {
            const hidden = node.attributes.properties.some(
              (prop) =>
                ts.isJsxAttribute(prop) &&
                prop.name.getText(file) === "aria-hidden" &&
                prop.initializer &&
                ts.isStringLiteral(prop.initializer) &&
                prop.initializer.text === "true",
            );
            const labelled = node.attributes.properties.some(
              (prop) => ts.isJsxAttribute(prop) && prop.name.getText(file) === "aria-label",
            );
            if (!hidden && !labelled) violations.push(`${path}: ${tag} lacks accessible intent`);
          }
        }
        // Ignore comments, prose punctuation, keyboard notation and sender HTML. These are
        // the legacy interface symbols, including their numeric HTML entity spellings.
        if (
          (ts.isJsxText(node) || ts.isStringLiteral(node)) &&
          /[←→↑↓↗↘↙↖‹›«»✓✔✕✖⚙🔍▲▼▶◀▾▸⌄×↻↩↪⟳🔗⋮⋯✎✉📎☰]|&#(?:8617|128206);/u.test(node.text)
        ) {
          violations.push(`${path}: legacy interface symbol ${node.text.trim()}`);
        }
        ts.forEachChild(node, visit);
      }
      visit(file);
    }
    expect(violations).toEqual([]);
    expect(icons.size).toBeGreaterThanOrEqual(23);
    const pkg = JSON.parse(readFileSync("package.json", "utf8"));
    expect(pkg.dependencies["@tabler/icons-react"]).toBe("3.50.0");
    expect(pkg.dependencies["@heroicons/react"]).toBeUndefined();
  });

  it("uses library-rendered masks for native disclosures, including static signature folds", () => {
    const css = readFileSync("src/styles.css", "utf8");
    const masks = [...css.matchAll(/mask-image:url\("data:image\/svg\+xml,([^"]+)"\)/g)].map(
      (match) => decodeURIComponent(match[1]!),
    );
    expect(masks).toEqual([
      renderToStaticMarkup(createElement(IconChevronRight)),
      renderToStaticMarkup(createElement(IconChevronLeft)),
      renderToStaticMarkup(createElement(IconChevronRight)),
    ]);
    const disclosures = css.slice(css.indexOf(".pan > summary"), css.indexOf(".bub p"));
    expect(disclosures).not.toMatch(/border-(?:left|right)-color:/);
    expect(css).toContain(".srcgrp[open] > summary::before { transform:rotate(90deg); }");
    expect(css).toContain(".htail::after { transform:rotate(-90deg);");
    expect(css).toContain(".bd details.sig[open] > summary::before { transform:rotate(90deg); }");
  });

  it("keeps compact counters at 11px with labels and default Tabler stroke", () => {
    for (const [element, glyph, label] of [
      [
        createElement(AttachmentCount, { attachments: 2 }),
        "paperclip",
        "2 attachments in this thread",
      ],
      [createElement(MailCount, { entries: 2 }), "mail", "2 messages in this thread"],
      [createElement(PeopleCount, { people: 2 }), "users", "2 people in this thread"],
    ] as const) {
      const html = renderToStaticMarkup(element);
      expect(html).toContain(`tabler-icon-${glyph}`);
      expect(html).toContain('width="11" height="11"');
      expect(html).toContain('stroke-width="2"');
      expect(html).toContain('aria-hidden="true"');
      expect(html).toContain(label);
    }
  });

  it("retains reply destinations and accessible attachment and merge meanings", () => {
    const reply = renderToStaticMarkup(
      createElement(ReplyTarget, {
        answerAnchor: "message-1",
        words: { who: "Sender", when: "Today" },
      }),
    );
    expect(reply).toContain('href="#message-1"');
    expect(reply).toContain('aria-label="Jump to the message being replied to: Sender, Today"');
    expect(reply).toContain("tabler-icon-arrow-back-up");
    const parent = renderToStaticMarkup(
      createElement(ReplyLink, {
        parent: { anchor: "parent", who: "Sender", when: "Today" },
      }),
    );
    expect(parent).toContain('href="#parent"');
    expect(parent).toContain("tabler-icon-arrow-back-up");
    const attachment = renderToStaticMarkup(
      createElement(Attachments, {
        attachments: [{ name: "file.txt", size: "1 B", kind: "text" }],
      }),
    );
    expect(attachment).toContain('aria-label="attachments"');
    expect(attachment).toContain("tabler-icon-paperclip");
    const merge = renderToStaticMarkup(
      createElement(MergeConfirm, {
        chosen: [
          {
            keepId: 1,
            dropId: 2,
            keepName: "Keeper",
            dropName: "Dropped",
            rule: "same-name",
            applicable: true,
          },
        ],
        onConfirm: () => undefined,
        onCancel: () => undefined,
      }),
    );
    expect(merge).toContain("tabler-icon-arrow-right");
    expect(merge).toContain('class="sr-only">into</span>');
    expect(merge).toContain("This cannot be undone");
  });
});
