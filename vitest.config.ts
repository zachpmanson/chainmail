import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    environment: "node",
    include: ["test/**/*.test.ts"],
    // `nix develop` leaves a source snapshot in .direnv that would run as a second suite.
    exclude: ["**/node_modules/**", "**/.direnv/**"],
  },
});
