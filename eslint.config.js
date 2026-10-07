import js from "@eslint/js";
import betterTailwind from "eslint-plugin-better-tailwindcss";
import { getDefaultSelectors } from "eslint-plugin-better-tailwindcss/defaults";
import { MatcherType, SelectorKind } from "eslint-plugin-better-tailwindcss/types";
import reactHooks from "eslint-plugin-react-hooks";
import globals from "globals";
import tseslint from "typescript-eslint";

export default tseslint.config(
  {
    ignores: [
      "cmd/server/dist/**",
      "node_modules/**",
      "src/lib/api/api.d.ts",
      "src/lib/timeline/spec.d.ts",
    ],
  },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ["**/*.{ts,tsx}"],
    languageOptions: {
      globals: {
        ...globals.browser,
        ...globals.node,
      },
    },
    plugins: {
      "react-hooks": reactHooks,
    },
    rules: {
      "no-undef": "off",
      "react-hooks/exhaustive-deps": "warn",
      "react-hooks/rules-of-hooks": "error",
    },
  },
  {
    files: ["src/**/*.{ts,tsx}", "scripts/**/*.tsx"],
    plugins: { "better-tailwindcss": betterTailwind },
    settings: {
      "better-tailwindcss": {
        entryPoint: "src/tailwind.css",
        selectors: [
          ...getDefaultSelectors(),
          // Shared class constants and variant maps, e.g. buttonBase, buttonVariants, tones.
          {
            kind: SelectorKind.Variable,
            name: "^(?:\\w*(?:Base|Button|Classes|Variants)|tones)$",
            match: [{ type: MatcherType.String }, { type: MatcherType.ObjectValue }],
          },
        ],
      },
    },
    rules: {
      "better-tailwindcss/enforce-canonical-classes": "error",
      "better-tailwindcss/enforce-shorthand-classes": "error",
      "better-tailwindcss/no-conflicting-classes": "error",
      "better-tailwindcss/no-deprecated-classes": "error",
      "better-tailwindcss/no-duplicate-classes": "error",
      "better-tailwindcss/no-unknown-classes": [
        "error",
        {
          // Hooks for styles.css, select.css, behaviour.ts and tests, not Tailwind utilities.
          ignore: [
            "^(?:av|bd|buildslot|chdr|chsec|chstart|end|foot2|has-choice|hdr|hrow|htail|ibbuild|ibread|ibread-head|ibsplit|lk|mbody|mini|navcancel|navright|navsearch|nd|open|pan|par|parlbl|people|replies|replyhtml|sitehead|sitenav|spine|srcgrp|stream|sys|top|tstart|vrow|wrap)$",
          ],
        },
      ],
      "better-tailwindcss/no-unnecessary-whitespace": "error",
    },
  },
);
