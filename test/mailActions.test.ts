import { MutationObserver, QueryClient, QueryObserver } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";
import { mailWriteOptions } from "../src/lib/inbox/mailActions";
import { dropFromLists, markInLists, putBackLists } from "../src/lib/inbox/lists";
import { searchKey } from "../src/lib/api/queryKeys";

const inboxKey = [...searchKey, { params: { query: { label: "INBOX" } } }];
const chain = (rootExtId: string) => ({ rootExtId, unread: 1, entries: 1 });
const page = (...roots: string[]) => ({ chains: roots.map(chain) });

function deferred() {
  let resolve!: () => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<void>((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}

function setup() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  client.setQueryData(inboxKey, page("a", "b", "c"));
  let server = page("a", "b", "c");
  const queryFn = vi.fn(async () => server);
  const observer = new QueryObserver(client, {
    queryKey: inboxKey,
    queryFn,
    staleTime: Infinity,
  });
  const unsubscribe = observer.subscribe(() => {});
  const rows = () => client.getQueryData<ReturnType<typeof page>>(inboxKey)!.chains;
  const roots = () => rows().map((row) => row.rootExtId);
  const write = (root: string, read = false) => {
    const request = deferred();
    const started = deferred();
    const mutation = new MutationObserver(client, {
      ...mailWriteOptions<void>(client, () => {
        const was = read ? markInLists(client, root, false) : dropFromLists(client, [root]);
        started.resolve();
        return was;
      }),
      mutationFn: () => request.promise,
      onError: (_error, _variables, context) => {
        if (context) putBackLists(client, context.was);
      },
    });
    const done = mutation.mutate().catch(() => {});
    return { ...request, started: started.promise, done };
  };
  return {
    client,
    queryFn,
    roots,
    rows,
    write,
    server: (data: ReturnType<typeof page>) => {
      server = data;
    },
    close: () => {
      unsubscribe();
      client.clear();
    },
  };
}

describe("overlapping mailbox writes", () => {
  it.each([false, true])("waits for both archives (reverse completion: %s)", async (reverse) => {
    const s = setup();
    try {
      const a = s.write("a");
      await a.started;
      const b = s.write("b");
      await b.started;
      expect(s.roots()).toEqual(["c"]);

      const [first, last] = reverse ? [b, a] : [a, b];
      s.server(reverse ? page("a", "c") : page("b", "c"));
      first.resolve();
      await first.done;
      expect(s.queryFn).not.toHaveBeenCalled();
      expect(s.roots()).toEqual(["c"]);

      s.server(page("c"));
      last.resolve();
      await last.done;
      expect(s.queryFn).toHaveBeenCalledTimes(1);
      expect(s.roots()).toEqual(["c"]);
    } finally {
      s.close();
    }
  });

  it.each([false, true])("reconciles same-turn settlements (last fails: %s)", async (fails) => {
    const s = setup();
    try {
      const a = s.write("a");
      await a.started;
      const b = s.write("b");
      await b.started;
      s.server(fails ? page("b", "c") : page("c"));
      a.resolve();
      if (fails) b.reject(new Error("refused"));
      else b.resolve();
      await Promise.all([a.done, b.done]);
      expect(s.queryFn).toHaveBeenCalledTimes(1);
      expect(s.roots()).toEqual(fails ? ["b", "c"] : ["c"]);
    } finally {
      s.close();
    }
  });

  it("still reconciles a successful archive when the last write fails", async () => {
    const s = setup();
    try {
      const a = s.write("a");
      await a.started;
      const b = s.write("b");
      await b.started;
      a.resolve();
      await a.done;
      expect(s.queryFn).not.toHaveBeenCalled();

      s.server(page("b", "c"));
      b.reject(new Error("refused"));
      await b.done;
      expect(s.queryFn).toHaveBeenCalledTimes(1);
      expect(s.roots()).toEqual(["b", "c"]);
    } finally {
      s.close();
    }
  });

  it("does not roll an earlier failure back over a later optimistic archive", async () => {
    const s = setup();
    try {
      const a = s.write("a");
      await a.started;
      const b = s.write("b");
      await b.started;
      a.reject(new Error("refused"));
      await a.done;
      expect(s.roots()).toEqual(["c"]);
      expect(s.queryFn).not.toHaveBeenCalled();

      s.server(page("a", "c"));
      b.resolve();
      await b.done;
      expect(s.roots()).toEqual(["a", "c"]);
    } finally {
      s.close();
    }
  });

  it("does not let a read-state write refetch over an archive", async () => {
    const s = setup();
    try {
      const read = s.write("a", true);
      await read.started;
      const archive = s.write("b");
      await archive.started;
      read.resolve();
      await read.done;
      expect(s.queryFn).not.toHaveBeenCalled();
      expect(s.roots()).toEqual(["a", "c"]);
      expect(s.rows()[0]?.unread).toBe(0);

      s.server({ chains: [{ ...chain("a"), unread: 0 }, chain("c")] });
      archive.resolve();
      await archive.done;
      expect(s.roots()).toEqual(["a", "c"]);
      expect(s.rows()[0]?.unread).toBe(0);
    } finally {
      s.close();
    }
  });
});

describe("optimistic list rollback", () => {
  it("restores an isolated failed write, including infinite-query pages", () => {
    const client = new QueryClient();
    try {
      const before = { pages: [page("a"), page("b", "c")], pageParams: ["", "next"] };
      client.setQueryData(inboxKey, before);
      const was = dropFromLists(client, ["a", "b"]);
      expect(client.getQueryData(inboxKey)).toEqual({
        pages: [page(), page("c")],
        pageParams: before.pageParams,
      });
      putBackLists(client, was);
      expect(client.getQueryData(inboxKey)).toEqual(before);
    } finally {
      client.clear();
    }
  });

  it("does not restore unchanged caches over later writes", () => {
    const client = new QueryClient();
    try {
      client.setQueryData(inboxKey, page("a", "b"));
      const was = dropFromLists(client, ["missing"]);
      dropFromLists(client, ["b"]);
      putBackLists(client, was);
      expect(client.getQueryData(inboxKey)).toEqual(page("a"));
    } finally {
      client.clear();
    }
  });
});
