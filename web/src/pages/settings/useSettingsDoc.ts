import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { basePath } from "../../api/client";
import { useToast } from "../../lib/toast";

type Doc = "media" | "downloads" | "cleanup" | "readsync" | "general";

/** useSettingsDoc loads a settings document into editable local state. */
export function useSettingsDoc<T extends object>(name: Doc) {
  const qc = useQueryClient();
  const toast = useToast();
  const url = `${basePath}/api/v1/settings/${name}`;
  const { data, isLoading, error } = useQuery({
    queryKey: ["settings", name],
    queryFn: async () => {
      const r = await fetch(url);
      if (!r.ok) throw new Error(`HTTP ${r.status}`);
      return (await r.json()) as T;
    },
  });
  const [value, setValue] = useState<T | null>(null);
  const [saving, setSaving] = useState(false);
  useEffect(() => {
    if (data) setValue(data);
  }, [data]);
  const save = async (v: T = value as T) => {
    setSaving(true);
    try {
      const r = await fetch(url, { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify(v) });
      if (!r.ok) {
        const body = await r.json().catch(() => ({}));
        throw new Error(body.detail || body.title || `HTTP ${r.status}`);
      }
      qc.invalidateQueries({ queryKey: ["settings", name] });
      toast.success("Settings saved");
    } catch (e) {
      toast.fromError(e, "Could not save settings");
    } finally {
      setSaving(false);
    }
  };
  const patch = (p: Partial<T>) => setValue((v) => (v ? { ...v, ...p } : v));
  return { value, setValue, patch, save, saving, isLoading, error };
}
