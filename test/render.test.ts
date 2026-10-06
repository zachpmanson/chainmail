import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { afterEach, describe, expect, it } from "vitest";

const tempDirs: string[] = [];
afterEach(() => {
  for (const directory of tempDirs.splice(0)) {
    rmSync(directory, { recursive: true, force: true });
  }
});

describe("standalone renderer styles", () => {
  it("embeds compiled Tailwind utilities without an external stylesheet", () => {
    const directory = mkdtempSync(join(tmpdir(), "chainmail-render-"));
    tempDirs.push(directory);
    const output = join(directory, "page.html");

    execFileSync(process.execPath, [
      "--import",
      "tsx",
      "scripts/render.tsx",
      "fixtures/synthetic.json",
      "-o",
      output,
    ], { cwd: resolve(import.meta.dirname, ".."), stdio: "pipe" });

    const html = readFileSync(output, "utf8");
    expect(html).toContain(".fixed {");
    expect(html).toContain("border-radius: 10px;");
    expect(html).toContain("background-color: var(--card);");
    expect(html).toContain("@media print");
    expect(html).toContain("max-\\[1024px\\]");
    expect(html).not.toMatch(/<link[^>]+stylesheet/i);
  });
});
