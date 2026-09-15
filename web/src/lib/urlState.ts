import { useCallback } from "react";
import { useSearchParams, type SetURLSearchParams } from "react-router";

// Updates made in the same tick (e.g. a filter plus "back to page 1") are
// applied together: setSearchParams computes from the URL of the last render,
// so two separate calls would make the second undo the first.
let pending: { updates: Map<string, string | null>; replace: boolean; set: SetURLSearchParams } | null = null;

function queueParam(set: SetURLSearchParams, name: string, value: string | null, replace: boolean) {
  if (!pending) {
    pending = { updates: new Map(), replace: true, set };
    queueMicrotask(() => {
      const p = pending!;
      pending = null;
      p.set(
        (prev) => {
          const n = new URLSearchParams(prev);
          for (const [k, v] of p.updates) {
            if (v === null) n.delete(k);
            else n.set(k, v);
          }
          return n;
        },
        { replace: p.replace },
      );
    });
  }
  pending.updates.set(name, value);
  pending.replace = pending.replace && replace; // any history entry wins
}

/**
 * useQueryParam keeps a value in the URL query string (so it survives reloads
 * and can be shared). Setting the default value removes the parameter.
 */
export function useQueryParam(name: string, def = ""): [string, (v: string, opts?: { replace?: boolean }) => void] {
  const [params, setParams] = useSearchParams();
  const value = params.get(name) ?? def;
  const set = useCallback(
    (v: string, opts?: { replace?: boolean }) => queueParam(setParams, name, v === def || v === "" ? null : v, opts?.replace ?? true),
    [name, def, setParams],
  );
  return [value, set];
}

/** sessionState stores small JSON values per browser tab. */
export const sessionState = {
  get<T>(key: string, fallback: T): T {
    try {
      const v = sessionStorage.getItem(key);
      return v ? (JSON.parse(v) as T) : fallback;
    } catch {
      return fallback;
    }
  },
  set(key: string, v: unknown) {
    try {
      sessionStorage.setItem(key, JSON.stringify(v));
    } catch {
      /* storage full or disabled */
    }
  },
  remove(key: string) {
    try {
      sessionStorage.removeItem(key);
    } catch {
      /* ignore */
    }
  },
};
