import { useCallback } from "react";
import { useSearchParams } from "react-router";

/**
 * useQueryParam keeps a value in the URL query string (so it survives reloads
 * and can be shared). Setting the default value removes the parameter.
 */
export function useQueryParam(name: string, def = ""): [string, (v: string, opts?: { replace?: boolean }) => void] {
  const [params, setParams] = useSearchParams();
  const value = params.get(name) ?? def;
  const set = useCallback(
    (v: string, opts?: { replace?: boolean }) =>
      setParams(
        (p) => {
          const n = new URLSearchParams(p);
          if (v === def || v === "") n.delete(name);
          else n.set(name, v);
          return n;
        },
        { replace: opts?.replace ?? true },
      ),
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
