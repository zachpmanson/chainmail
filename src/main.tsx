import { createRoot } from "react-dom/client";
import { StrictMode } from "react";
import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { router } from "./router";
import { makeQueryClient } from "./lib/queryClient";
import "./styles.css";
import "./select.css";

const root = document.getElementById("root");
// The router resolves the current URL and builds its initial match tree
// asynchronously; render only once it has somewhere to go.
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