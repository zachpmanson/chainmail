import { useEffect, useState } from "react";
import { PALETTE, readPalette, type Palette as Readings } from "../../lib/ui/palette";

/** Swatches use resolved values, not `var(--x)`, which would paint the off theme's
 *  rows in the current theme's colour. */
export default function Palette() {
  const [palette, setPalette] = useState<Readings | null>(null);
  useEffect(() => setPalette(readPalette()), []);

  return (
    <div className="max-w-full overflow-x-auto">
      <table className="min-w-[36rem] border-collapse mt-2 text-xs">
        <thead>
          <tr>
            <th className="border-b border-line pb-1 pr-4 text-left text-2xs font-bold uppercase tracking-[.08em] text-muted last:pr-0">
              colour
            </th>
            <th className="border-b border-line pb-1 pr-4 text-left text-2xs font-bold uppercase tracking-[.08em] text-muted last:pr-0">
              light
            </th>
            <th className="border-b border-line pb-1 pr-4 text-left text-2xs font-bold uppercase tracking-[.08em] text-muted last:pr-0">
              dark
            </th>
            <th className="border-b border-line pb-1 pr-4 text-left text-2xs font-bold uppercase tracking-[.08em] text-muted last:pr-0">
              used for
            </th>
          </tr>
        </thead>
        <tbody>
          {PALETTE.map(({ name, what }) => (
            <tr key={name}>
              <td className="border-b border-line py-1 pr-4 align-middle whitespace-nowrap last:pr-0">
                <code className="font-[inherit] text-xs">--{name}</code>
              </td>
              <td className="border-b border-line py-1 pr-4 align-middle whitespace-nowrap last:pr-0">
                <Swatch value={palette?.light[name]} />
              </td>
              <td className="border-b border-line py-1 pr-4 align-middle whitespace-nowrap last:pr-0">
                <Swatch value={palette?.dark[name]} />
              </td>
              <td className="border-b border-line py-1 pr-4 align-middle whitespace-normal text-muted last:pr-0">
                {what}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function Swatch({ value }: { value?: string }) {
  return (
    <>
      <span
        className="mr-1.5 inline-block h-[.82rem] w-4 align-[-.09rem] rounded-sm border border-line"
        style={value ? { background: value } : undefined}
        aria-hidden="true"
      />
      <span className="tabular-nums text-muted">{value ? value : "—"}</span>
    </>
  );
}
