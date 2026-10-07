import { describe, expect, it } from "vitest";
import { derive } from "../src/lib/timeline/derive";
import { graphLanes } from "../src/lib/timeline/lanes";
import {
  capD,
  linkD,
  pos,
  size,
  treeFileName,
  treeSvgString,
  type TreeExport,
} from "../src/lib/timeline/treeSvg";
import { msg, timeline } from "./fixtures";

describe("geometry", () => {
  it("transposes position between orientations", () => {
    expect(pos("v", 2, 1)).toEqual([22, 33]);
    expect(pos("h", 2, 1)).toEqual([39, 25]);
  });

  it("sizes a vertical tree no narrower than the minimum", () => {
    expect(size("v", 1, 1)).toEqual([96, 19]);
    expect(size("v", 3, 10)).toEqual([123, 43]);
    expect(size("h", 3, 2)).toEqual([52, 35]);
  });

  it("draws a straight link within a lane and a curve across lanes", () => {
    expect(linkD("v", 11, 9, 11, 21)).toBe("M11 13.4 V16.6");
    expect(linkD("v", 11, 9, 22, 21)).toBe("M11 13.4 V16.5 Q11 21 15.5 21 H18");
    expect(linkD("h", 11, 9, 25, 9)).toBe("M15.4 9 H20.6");
    expect(capD("h", 11, 9)).toBe("M4.4 4.4 V13.6");
  });
});

describe("treeFileName", () => {
  it("slugs the title, with a fallback", () => {
    expect(treeFileName("##Daystrom CSV!")).toBe("reply-tree-daystrom-csv.svg");
    expect(treeFileName("###")).toBe("reply-tree-tree.svg");
  });
});

describe("treeSvgString", () => {
  const exported = (dark: boolean): TreeExport => {
    const v = derive(
      timeline(
        msg("a", { sender: "Ada", org: "Loom" }),
        msg("b", { parent: "a", time: "10:00" }),
        msg("c", { parent: "a", time: "11:00", quoted: true }),
        msg("n", { kind: "note", label: "Call", time: "12:00" }),
      ),
    );
    const g = graphLanes(
      v.rows.map((r) => r.entry),
      (e) => v.rows.find((r) => r.entry === e)!.id,
    );
    return {
      title: 'Ada & "Bo"',
      rows: v.rows,
      nodes: g.nodes,
      laneCount: g.laneCount,
      deepest: 1,
      dark,
    };
  };

  it("escapes the title and tallies the tree", () => {
    const svg = treeSvgString(exported(false));
    expect(svg.startsWith('<svg xmlns="http://www.w3.org/2000/svg"')).toBe(true);
    expect(svg).toContain("<title>Ada &amp; &quot;Bo&quot; reply tree</title>");
    const tally = [...svg.matchAll(/<tspan[^>]*>(\d+)<\/tspan> ([a-z ]+)</g)].map(
      (m) => `${m[1]} ${m[2]}`,
    );
    expect(tally).toEqual(["2 chains", "1 lanes", "1 deep", "2 forks", "3 dead ends"]);
  });

  it("draws one mark per row: hollow for quoted, a diamond for notes", () => {
    const svg = treeSvgString(exported(false));
    const marks = svg.split('<g transform="')[1]!.split("</g>")[0]!;
    expect(marks.match(/<circle /g)).toHaveLength(3);
    expect(marks.match(/<circle [^>]*fill="#fff" stroke="#77716a"/g)).toHaveLength(1);
    expect(marks.match(/rotate\(45/g)).toHaveLength(1);
  });

  it("bakes in the dark palette", () => {
    expect(treeSvgString(exported(true))).toContain('fill="#1d1c21"');
    expect(treeSvgString(exported(false))).not.toContain("#1d1c21");
  });
});
