import { infiniteQueryOptions } from "@tanstack/react-query";
import { api, unwrap } from "../../api/client";
import { nextShelfCursor, type Shelf, type ShelfFilters } from "./shelfState";

export function shelfQueryOptions(shelf: Shelf, filters: ShelfFilters, viewer: string) {
  return infiniteQueryOptions({
    queryKey: ["discover", "shelf", viewer, shelf, filters],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) => unwrap(api.GET("/api/v1/discover/{shelf}", {
      params: { path: { shelf }, query: { ...filters, pageSize: 36, cursor: pageParam } },
      signal,
    })),
    getNextPageParam: nextShelfCursor,
    staleTime: 5 * 60_000,
    refetchOnWindowFocus: false,
    retry: false,
  });
}
