import { createRoot } from "react-dom/client";
import { StrictMode } from "react";
import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { router } from "./router";
import { makeQueryClient } from "./lib/api/queryClient";
import "./styles.css";
import "./select.css";
import "./tailwind.css";

const root = document.getElementById("root");
// The router resolves its initial match asynchronously; render once it has.
void router.load().then(() => {
  if (!root) return;
  createRoot(root).render(
    <StrictMode>
      <QueryClientProvider client={makeQueryClient()}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </StrictMode>,
  );
});
