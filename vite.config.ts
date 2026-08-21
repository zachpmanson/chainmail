import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { homedir } from "node:os";

// The dev server is for iterating on the view against a spec on disk.
// Shipping artifacts come from `npm run render` (scripts/render.tsx), which
// server-renders a spec to one self-contained HTML file.
export default defineConfig({
  plugins: [react()],
  publicDir: "fixtures",
  define: {
    // lets the client expand a leading ~ in ?spec=
    __HOME__: JSON.stringify(homedir()),
  },
  server: {
    open: "/?spec=/synthetic.json",
    // The API is a separate localhost process, so /v1 is proxied rather than
    // called cross-origin: a same-origin client needs no CORS allowance on the
    // service, and widening the service's origin policy is the one change that
    // would make its unsanitised sender HTML reachable from another page.
    proxy: {
      "/v1": {
        target: process.env.CHAINMAIL_API ?? "http://127.0.0.1:8765",
      },
    },
    fs: {
      // Absolute paths passed as ?spec= are fetched through Vite's /@fs/ route,
      // which refuses anything outside this allow-list. Scoped to the home
      // directory: specs are personal files, and this server is localhost-only.
      allow: [process.cwd(), homedir()],
    },
  },
});
