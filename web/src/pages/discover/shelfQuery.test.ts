import { InfiniteQueryObserver, QueryClient } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { shelfQueryOptions } from "./shelfQuery";

const request = vi.hoisted(() => ({ get: vi.fn() }));
vi.mock("../../api/client", () => ({ api: { GET: request.get }, unwrap: (promise: Promise<unknown>) => promise }));
const page = (nextCursor?: string) => ({ library: [], popular: [], sourceErrors: [], nextCursor });
const clients: QueryClient[] = [];
function client() {
  const qc = new QueryClient({ defaultOptions: { queries: { gcTime: 0 } } });
  clients.push(qc);
  return qc;
}
afterEach(() => { clients.forEach((qc) => qc.clear()); clients.length = 0; request.get.mockReset(); });

describe("Discover cursor paging", () => {
  it("follows empty pages, preserves filters and stops when there is no cursor", async () => {
    request.get.mockResolvedValueOnce(page("first")).mockResolvedValueOnce(page("second")).mockResolvedValueOnce(page());
    const qc = client();
    const filters = { sort: "popularity" as const, lang: "uk", inLibrary: "false" as const };
    const options = shelfQueryOptions("popular", filters, "reader:7");
    await qc.fetchInfiniteQuery(options);
    const observer = new InfiniteQueryObserver(qc, options);
    expect(observer.getCurrentResult().hasNextPage).toBe(true);
    await observer.fetchNextPage();
    await observer.fetchNextPage();
    expect(observer.getCurrentResult().hasNextPage).toBe(false);
    expect(observer.getCurrentResult().data?.pageParams).toEqual([undefined, "first", "second"]);
    for (const [index, cursor] of [undefined, "first", "second"].entries()) {
      expect(request.get.mock.calls[index][1].params.query).toEqual({ ...filters, pageSize: 36, cursor });
      expect(request.get.mock.calls[index][1].signal).toBeInstanceOf(AbortSignal);
    }
    observer.destroy();
  });

  it("starts a new cursor chain when filters, shelf or viewer change", async () => {
    request.get.mockResolvedValue(page("next"));
    const qc = client();
    for (const options of [shelfQueryOptions("recommendations", { sort: "title" }, "reader:7"), shelfQueryOptions("recommendations", { sort: "newest" }, "reader:7"), shelfQueryOptions("recently-updated", { sort: "title" }, "reader:7"), shelfQueryOptions("recommendations", { sort: "title" }, "reader:8")]) {
      await qc.fetchInfiniteQuery(options);
    }
    expect(request.get).toHaveBeenCalledTimes(4);
    expect(request.get.mock.calls.every((call) => call[1].params.query.cursor === undefined)).toBe(true);
  });

  it("keeps existing pages on 410 and restarts at the first page without replaying old cursors", async () => {
    request.get.mockResolvedValueOnce(page("expired")).mockRejectedValueOnce(Object.assign(new Error("expired"), { status: 410 })).mockResolvedValueOnce(page());
    const qc = client();
    const options = shelfQueryOptions("popular", { sort: "popularity" }, "reader:7");
    await qc.fetchInfiniteQuery(options);
    const observer = new InfiniteQueryObserver(qc, options);
    const unsubscribe = observer.subscribe(() => {});
    await observer.fetchNextPage();
    expect(observer.getCurrentResult().isFetchNextPageError).toBe(true);
    expect(observer.getCurrentResult().data?.pages).toHaveLength(1);
    expect(request.get).toHaveBeenCalledTimes(2);
    await qc.resetQueries({ queryKey: options.queryKey, exact: true });
    expect(request.get.mock.calls[2][1].params.query.cursor).toBeUndefined();
    expect(observer.getCurrentResult().data?.pages).toHaveLength(1);
    expect(observer.getCurrentResult().isError).toBe(false);
    unsubscribe();
    observer.destroy();
  });
});
