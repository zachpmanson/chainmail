import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The dev server is for iterating on the view against a fixture spec.
// Shipping artifacts are produced by `npm run render` (see scripts/render.tsx),
// which server-renders a spec to one self-contained HTML file.
export default defineConfig({
  plugins: [react()],
  server: { open: "/?spec=/synthetic.json" },
  publicDir: "fixtures",
});
