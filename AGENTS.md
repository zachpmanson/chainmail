# Agent notes

## Before pushing

Run `npm run lint` and fix everything it reports before you push. It enforces the Tailwind rules:
- known, canonical, non-conflicting classes;
- the type scale, so no arbitrary `text-[…rem]`, `leading-[…]`, `rounded-[…px]` or `opacity-[…]`.

`make check` runs the same lint along with everything else CI runs.
