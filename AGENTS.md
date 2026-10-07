# Agent notes

## Before pushing

Run `npm run lint` and fix everything it reports before you push. It enforces the Tailwind rules:
- known, canonical, non-conflicting classes;
- the type scale, so no arbitrary `text-[…rem]`, `leading-[…]`, `rounded-[…px]` or `opacity-[…]`.

`make check` runs the same lint along with everything else CI runs.

Prettier formats everything, including class order (`prettier-plugin-tailwindcss`), so don't hand-sort
classes. Lint and format run in CI, not in pre-commit hooks.

## Frontend style

How the frontend is written, as settled during the Tailwind migration (#301 onward). Lint enforces what
it can; the rest is convention. When the two disagree, fix the lint.

### Styling

#### Tailwind, without Preflight

Styling is Tailwind v4 utilities in `className`. `src/tailwind.css` imports the theme and utilities but
**not Preflight**: rendered sender HTML relies on browser default styles. So elements keep their UA
styles, and a control that should look like nothing says so (`variant="bare"` on `Button` exists for this).

#### Tokens

Colours are theme tokens backed by CSS variables, so light and dark switch in one place (`styles.css`):

| Token                   | Use                                        |
| ----------------------- | ------------------------------------------ |
| `bg`, `card`            | page and raised surfaces                   |
| `fg`, `muted`, `strong` | text: body, secondary, emphasised-positive |
| `line`, `dash`          | borders and rules                          |
| `accent`                | interactive and focus colour               |
| `mine`, `quote`         | the reader's own items; quoted text        |
| `org-1` … `org-5`       | per-organisation colours in timelines      |

Write `text-muted`, `border-line`, `bg-card`, never `text-[var(--muted)]` or a hex value. Red is the one
stock colour in use, for errors and destructive actions (`text-red-700`, `border-red-700`).

A new token goes in three places: the variable in `styles.css` (light and both dark blocks),
`@theme inline` in `tailwind.css`, and the `color` list in `src/lib/ui/cn.ts`.

#### Stay on the scale

Lint rejects arbitrary values for these; pick the nearest step instead:

- **Text size:** `text-2xs` (our one custom step, for labels, badges and meta), `text-xs`, `text-sm`, then the
  defaults. `em` sizes like `text-[.9em]` are allowed where something should scale with its parent.
- **Line height:** take what the size step brings; otherwise `leading-tight/snug/normal` or `text-xs/snug`.
- **Radius:** `rounded-sm`, `rounded-md`, `rounded-lg`, `rounded-full`.
- **Opacity:** a scale step, e.g. `opacity-55`.

Keep spacing (`p-`, `m-`, `gap-`) on the scale too: `px-1.5`, not `px-[.4rem]`. Arbitrary values remain
for things with no scale: grid templates, shadows, one-off widths like `w-[8.6rem]`.

Lint also requires canonical, non-conflicting, non-duplicated, non-deprecated v4 classes, and shorthands
where they exist (`size-8`, not `w-8 h-8`).

#### Breakpoints

`min-[60rem]:` is the layout breakpoint: above it the inbox is two panes, below it one. Use it rather
than `md:`/`lg:` so components agree with the `60rem` media queries in `styles.css` and `select.css`.
Add other breakpoints only for a local fix.

#### Writing classes

- **Inline by default.** A class string used by one element goes straight in its `className`, not in a const.
- **Constants only when shared,** and named so lint finds them: a `…Base`, `…Button`, `…Classes` or `…Variants`
  suffix, or `tones` (see `ui/styles.ts` and `ui/StatusBadge.tsx`).
- **Overridable components take `className` and merge with `cn()`** (`src/lib/ui/cn.ts`, over tailwind-merge),
  so the caller's class beats a conflicting default regardless of CSS order.
- **Conditional classes:** use `cn(…, cond && "x")`, or a template with a leading space,
  `` `… base${cond ? " extra" : ""}` ``. Prettier keeps that space (`tailwindPreserveWhitespace`).
- **Group state over JS state:** `group/icon` with `group-hover/icon:`, `has-disabled:`, `aria-pressed:` and
  similar express hover, disabled and pressed looks without React state.

#### Hook class names

A few plain class names (`ibread`, `navsearch`, `mini`, `stream` …) are hooks for `styles.css`,
`client/behaviour.ts` or the static export, not utilities. They're listed in the `no-unknown-classes`
ignore list in `eslint.config.js`. Add one only when global CSS or the static page truly needs it,
and add it to that list. Don't leave class names that nothing defines.

To find elements from scripts and tests, prefer data attributes (`data-root`, `data-attachment`) over classes.

#### What stays in CSS

`styles.css` is shipped with the static transcript, so it holds what utilities can't express or
what the static page also needs:

- light/dark variables and base element styles;
- sender HTML inside `.bd` (and `body.plain` overrides of its inline styles);
- body-level modes set by script: `tree-h`, `chains`, `hasmap`, `popped`;
- `:has()` rules across components, disclosure markers (`::before`), view transitions.

`select.css` is browser-app-only layout (viewport-height panels). Component styling doesn't go in either
file: move it onto the element.

### UI primitives

Use `src/components/ui/` before writing control styles at a call site:

| Primitive                                                                      | For                                                                                                                     |
| ------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------- |
| `Button`                                                                       | `variant`: `secondary` (default), `primary`, `quiet`, `subtle`, `danger`, `menu`, `bare`; `density`: `normal`/`compact` |
| `ButtonLink`                                                                   | a link that looks like a `Button`                                                                                       |
| `IconButton`, `IconSelect`, `IconFrame`                                        | glyph-only controls (`IconButton` is a link when given `href`)                                                          |
| `TextInput`, `TextArea`, `SelectInput`, `Checkbox`, `CheckboxRow`, `FormField` | form controls on `fieldBase`                                                                                            |
| `InlineAlert`                                                                  | an error in place (`compact` for settings panels)                                                                       |
| `StatusBadge`, `DialogShell`, `ToastHost`                                      | badges, modals, toasts                                                                                                  |

Icons are Heroicons outline (`@heroicons/react/24/outline`), `aria-hidden`, inside an `IconButton`
with an `aria-label` and `title`. Interactive elements show focus with the shared
`focus-visible:outline-accent` ring.

### Components

- **One exported component per file,** named after it, `export default function Name`. Several per file only
  when they share module-level constants. Unexported helpers for one component stay beside it.
- **Props are an inline type, destructured in the signature:** `function C({ a, b }: { a: number; b: string })`.
  No named `Props` type unless it's shared. When props fall into domain groups, give the groups real types
  (`Message` takes a `MessageEmail` and named place/look types) rather than one long prop list.
- **Split oversized components** into focused components, hooks and pure functions. When a component's body
  is mostly effects and derived data, move that into named hooks (`useFolder`, `useThreadList`) and leave
  the component to lay things out.
- **Folders by feature:** `components/{compose,inbox,thread,specs,settings,ops,navigation,ui}`;
  `lib/{api,inbox,message,prefs,thread,timeline,ui,ops}`. `lib` never imports from `components`.
- **Hooks get their own file** (`useOwnToast.ts`), default-exported, in `lib` when not tied to one feature folder.

### State

- `useState` for local state.
- `useReducer` with a tagged `step` union for multi-step flows: compose and reply both go
  `editing → reviewing (→ sent)`, and a failed send shuts the send button.
- **zustand** for persisted preferences (`lib/prefs`, `persist` middleware) and for shared state that would
  otherwise be drilled through three or more layers (a context-scoped store for thread-wide state).
- **The URL** for in-page state worth linking or going Back to: open thread, folder, search. Use query params,
  not path changes.
- Reuse the shared hooks for patterns that recur: `useOwnToast`, `useBusyTask`, `useMeasuredWidth`, `useDrag`,
  `useLoadMore`.

### Comments

Short, and only where the reason isn't obvious from the code: a gotcha, an invariant, an outside constraint.
No restatements of the code, no history, no essays. Most functions need none.

### Tests

Vitest, in `test/`, against the pure modules in `lib`: concrete inputs and outputs, few mocks. When logic
inside a component is subtle enough to need a test (paging cursors, address parsing), extract it into a
pure function in `lib` and test that.
