import { MutationObserver, QueryObserver } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";
import { makeQueryClient } from "../src/lib/api/queryClient";
import { ApiError } from "../src/lib/api/api";

describe("shared retry safety", () => {
  it.each([
    "compose send",
    "reply send",
    "draft send",
    "draft create",
    "draft update",
    "draft delete",
  ])("does not replay %s after the write succeeds but its response is lost", async () => {
    const client = makeQueryClient();
    let writes = 0;
    const observer = new MutationObserver(client, {
      mutationFn: async () => {
        writes++;
        throw new TypeError("Failed to fetch after server commit");
      },
    });
    await expect(observer.mutate()).rejects.toThrow("Failed to fetch");
    expect(writes).toBe(1);
    client.clear();
  });

  it("does not retry rate-limited mutations either", async () => {
    const client = makeQueryClient();
    const write = vi.fn(async () => {
      throw new ApiError(429, "rate limited", 0);
    });
    const observer = new MutationObserver(client, { mutationFn: write });
    await expect(observer.mutate()).rejects.toThrow("rate limited");
    expect(write).toHaveBeenCalledTimes(1);
    client.clear();
  });

  it("retains safe read retries", async () => {
    const client = makeQueryClient();
    let reads = 0;
    const observer = new QueryObserver(client, {
      queryKey: ["safe-read"],
      retryDelay: 0,
      queryFn: async () => {
        if (++reads === 1) throw new ApiError(429, "rate limited", 0);
        return "ok";
      },
    });
    const result = await observer.refetch();
    expect(result.data).toBe("ok");
    expect(reads).toBe(2);
    client.clear();
  });
});
