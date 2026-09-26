import type { operations } from "../../api/schema";

export type Shelf = operations["discover-shelf"]["parameters"]["path"]["shelf"];
export type ShelfQuery = NonNullable<operations["discover-shelf"]["parameters"]["query"]>;
export type ShelfFilters = Omit<ShelfQuery, "cursor" | "pageSize">;
export const shelves: readonly Shelf[] = ["recommendations", "recently-updated", "popular"];
export const shelfSorts = {
  recommendations: ["recommended", "recently-updated", "newest", "title"],
  "recently-updated": ["recently-updated", "newest", "title"],
  popular: ["popularity", "recently-updated"],
} as const;

/** Only supported, validated filters reach the API, including from old saved URLs. */
export function parseShelfFilters(shelf: Shelf, params: URLSearchParams): ShelfFilters {
  const sort = params.get("sort");
  const result: ShelfFilters = { sort: shelfSorts[shelf].find((value) => value === sort) ?? shelfSorts[shelf][0] };
  for (const key of ["lang", "genre", "tag"] as const) {
    if (shelf === "popular" && key !== "lang") continue;
    const value = params.get(key)?.trim();
    if (value) result[key] = value;
  }
  const source = params.get("source");
  if (source && /^[1-9]\d*:.+$/.test(source)) result.source = source;
  for (const key of ["rootFolderId", "tagId"] as const) {
    if (shelf === "popular" && key === "tagId") continue;
    const value = Number(params.get(key));
    if (Number.isSafeInteger(value) && value > 0) result[key] = value;
  }
  const membership = params.get("inLibrary");
  if (membership === "true" || membership === "false") result.inLibrary = membership;
  if (shelf !== "popular") {
    result.format = (["manga", "manhwa", "manhua"] as const).find((v) => v === params.get("format"));
    result.status = (["unknown", "ongoing", "completed", "hiatus", "cancelled"] as const).find((v) => v === params.get("status"));
  }
  return result;
}

export function shelfParams(filters: ShelfFilters): URLSearchParams {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(filters)) {
    if (value !== undefined && value !== "") params.set(key, String(value));
  }
  return params;
}

/** A shared URL is complete state; saved choices apply only on a bare shelf URL. */
export function restoreShelfFilters(shelf: Shelf, search: string, saved: string | null): ShelfFilters {
  return parseShelfFilters(shelf, new URLSearchParams(search || saved || ""));
}

// An empty page can still have a continuation after server-side source filtering.
export function nextShelfCursor(page: { nextCursor?: string }): string | undefined {
  return page.nextCursor || undefined;
}

export function shouldLoadShelfPage(hasNext: boolean, fetching: boolean, error: boolean): boolean {
  return hasNext && !fetching && !error;
}
