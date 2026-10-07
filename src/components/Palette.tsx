import { useEffect, useState } from "react";
import { PALETTE, readPalette, type Palette as Readings } from "../lib/palette";

/**
 * The palette, as a table: every colour the client is drawn with, what each is
 * for, and what it resolves to in each theme.
 *
 * Both themes rather than the one in force, because that is the comparison a
 * palette is read for — a colour is chosen for being legible on its own
 * background, and the one-way-to-find-out is to see the two side by side. Which
 * theme the reader is currently in therefore changes nothing here, and the
 * table is read once.
 *
 * The swatch is drawn from the value read out of the stylesheet rather than
 * from `var(--x)`: half of these rows belong to the theme that is off, and a
 * swatch painted with the variable would show the lit theme's colour twice.
 */
export function Palette() {
  // Read after mount, not during render: the read asks the browser for the
  // resolved values, and doing that while rendering would be a side effect in
  // the one place React is entitled to run twice.
  const [palette, setPalette] = useState<Readings | null>(null);
  useEffect(() => setPalette(readPalette()), []);

  return (
    <div className="max-w-full overflow-x-auto">
      <table className="min-w-[36rem] border-collapse mt-[.55rem] text-[.76rem]">
        <thead>
          <tr>
            <th className="border-b border-line pb-[.2rem] pr-[.9rem] text-left text-[.64rem] font-bold uppercase tracking-[.08em] text-muted last:pr-0">
              colour
            </th>
            <th className="border-b border-line pb-[.2rem] pr-[.9rem] text-left text-[.64rem] font-bold uppercase tracking-[.08em] text-muted last:pr-0">
              light
            </th>
            <th className="border-b border-line pb-[.2rem] pr-[.9rem] text-left text-[.64rem] font-bold uppercase tracking-[.08em] text-muted last:pr-0">
              dark
            </th>
            <th className="border-b border-line pb-[.2rem] pr-[.9rem] text-left text-[.64rem] font-bold uppercase tracking-[.08em] text-muted last:pr-0">
              used for
            </th>
          </tr>
        </thead>
        <tbody>
          {PALETTE.map(({ name, what }) => (
            <tr key={name}>
              <td className="border-b border-line py-[.22rem] pr-[.9rem] align-middle whitespace-nowrap last:pr-0">
                <code className="font-[inherit] text-[.74rem]">--{name}</code>
              </td>
              <td className="border-b border-line py-[.22rem] pr-[.9rem] align-middle whitespace-nowrap last:pr-0">
                <Swatch value={palette?.light[name]} />
              </td>
              <td className="border-b border-line py-[.22rem] pr-[.9rem] align-middle whitespace-nowrap last:pr-0">
                <Swatch value={palette?.dark[name]} />
              </td>
              <td className="border-b border-line py-[.22rem] pr-[.9rem] align-middle whitespace-normal text-muted last:pr-0">
                {what}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/**
 * One theme's value for one colour: the swatch, and the value beside it. The
 * text is what carries the meaning — the swatch is decoration over it, hidden
 * from the reader who cannot see the difference anyway — and a value the
 * stylesheet does not define reads as a dash rather than as white.
 */
function Swatch({ value }: { value?: string }) {
  return (
    <>
      <span
        className="mr-[.4rem] inline-block h-[.82rem] w-4 align-[-.09rem] rounded-[3px] border border-line"
        style={value ? { background: value } : undefined}
        aria-hidden="true"
      />
      <span className="tabular-nums text-muted">{value ? value : "—"}</span>
    </>
  );
}
