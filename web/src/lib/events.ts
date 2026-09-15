import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { basePath } from "../api/client";

// resource name (from the server) -> query key prefixes to invalidate
const map: Record<string, string[][]> = {
  series: [["series"], ["wanted"], ["calendar"]],
  chapter: [["series"], ["wanted"]],
  seriessource: [["series"]],
  queue: [["queue"], ["series"]],
  command: [["commands"], ["tasks"]],
  health: [["health"]],
  module: [["modules"], ["sources"], ["health"]],
  extension: [["extensions"], ["sources"]],
  // catalog set changed: cached searches/browses may include removed catalogs
  catalogs: [["sources"], ["catalogs"], ["source-search"], ["browse"], ["source-manga"]],
  cache: [["cache"], ["source-search"], ["browse"], ["source-manga"]],
  processing: [["processing"], ["health"]],
  settings: [["settings"]],
  readers: [["readers"], ["series"]],
  rootfolder: [["rootfolders"]],
  profile: [["profiles"]],
  tag: [["tags"]],
  blocklist: [["blocklist"]],
  import: [["imports"], ["import"], ["import-entries"]],
};

type Listener = (type: string, payload: unknown) => void;
const listeners = new Set<Listener>();

/** Subscribe to raw server events (e.g. to show toasts). */
export function onServerEvent(fn: Listener) {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

/** useLiveUpdates keeps TanStack Query caches fresh from the SSE stream. */
export function useLiveUpdates(enabled: boolean) {
  const qc = useQueryClient();
  useEffect(() => {
    if (!enabled) return;
    const pending = new Map<string, string[]>();
    let timer: number | undefined;
    const flush = () => {
      timer = undefined;
      for (const key of pending.values()) qc.invalidateQueries({ queryKey: key });
      pending.clear();
    };
    const es = new EventSource(basePath + "/api/v1/events");
    es.addEventListener("resource.changed", (ev) => {
      try {
        const e = JSON.parse((ev as MessageEvent).data);
        for (const key of map[e.payload?.name] ?? []) pending.set(key.join("/"), key);
        if (!timer) timer = window.setTimeout(flush, 400);
      } catch {
        /* ignore */
      }
    });
    for (const t of ["chapter.imported", "download.failed", "health.issue", "health.restored", "cleanup.done", "series.added", "processing.progress"]) {
      es.addEventListener(t, (ev) => {
        try {
          const e = JSON.parse((ev as MessageEvent).data);
          listeners.forEach((l) => l(t, e.payload));
        } catch {
          /* ignore */
        }
      });
    }
    es.onerror = () => {
      // EventSource reconnects on its own; refetch everything once it does
      es.onopen = () => qc.invalidateQueries();
    };
    return () => {
      es.close();
      if (timer) clearTimeout(timer);
    };
  }, [enabled, qc]);
}
