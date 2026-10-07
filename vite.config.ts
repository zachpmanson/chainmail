import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { fileURLToPath } from "node:url";

// `vite build` emits into cmd/server/dist/, which the Go server embeds.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  publicDir: "fixtures",
  build: {
    // go:embed is relative to cmd/server/ and cannot reach parent dirs.
    outDir: fileURLToPath(new URL("./cmd/server/dist", import.meta.url)),
    emptyOutDir: true,
  },
  server: {
    open: "/",
    // Same-origin proxy so the service needs no CORS: widening its origin policy would
    // expose its unsanitised sender HTML. `/auth` too, or Vite serves index.html for it.
    proxy: {
      "/v1": {
        target: process.env.CHAINMAIL_API ?? "http://127.0.0.1:8765",
      },
      "/auth": {
        target: process.env.CHAINMAIL_API ?? "http://127.0.0.1:8765",
      },
    },
  },
});
