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
    <table className="paltab">
      <thead>
        <tr>
          <th>colour</th>
          <th>light</th>
          <th>dark</th>
          <th>used for</th>
        </tr>
      </thead>
      <tbody>
        {PALETTE.map(({ name, what }) => (
          <tr key={name}>
            <td>
              <code>--{name}</code>
            </td>
            <td>
              <Swatch value={palette?.light[name]} />
            </td>
            <td>
              <Swatch value={palette?.dark[name]} />
            </td>
            <td className="palwhat">{what}</td>
          </tr>
        ))}
      </tbody>
    </table>
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
      <span className="palsw" style={value ? { background: value } : undefined} aria-hidden="true" />
      <span className="palval">{value ? value : "—"}</span>
    </>
  );
}
