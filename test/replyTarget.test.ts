import { describe, expect, it } from "vitest";
import { derive } from "../src/lib/timeline/derive";
import { replyTarget } from "../src/lib/timeline/replyTarget";
import { msg, timeline } from "./fixtures";

const view = () =>
  derive(
    timeline(
      msg("a", { sender: "Ada", fromEmail: "ada@loom.example", time: "09:00" }),
      msg("b", { parent: "a", sender: "Bo", time: "10:00" }),
      msg("n", { kind: "note", label: "Call", date: "Tue 3 Mar 2026" }),
      msg("c", { parent: "n", sender: "Cy", date: "Tue 3 Mar 2026", time: "12:00" }),
      msg("d", { parent: "gone", sender: "Di", date: "Wed 4 Mar 2026" }),
    ),
  );

const of = (id: string) => {
  const v = view();
  return replyTarget(
    v.rows.find((r) => r.id === id)!,
    v,
  );
};

describe("replyTarget", () => {
  it("names the parent message, its address and clock", () => {
    expect(of("b")).toEqual({
      anchor: "a",
      who: "Ada",
      whoTitle: "Ada <ada@loom.example>",
      when: "Mon 2 Mar 2026 09:00",
    });
  });

  it("names a note by its label, with no hover title", () => {
    expect(of("c")).toEqual({
      anchor: "n",
      who: "Call",
      whoTitle: undefined,
      when: "Tue 3 Mar 2026",
    });
  });

  it("is null at a thread start or for a parent not on the page", () => {
    expect(of("a")).toBeNull();
    expect(of("d")).toBeNull();
  });
});
