import { t } from "../../lib/i18n/core";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, unwrap } from "../../api/client";
import { ErrorBox, Input, Loading, Modal, Select, Switch } from "../../components/ui";
import { useToast } from "../../lib/toast";

/**
 * SourceSettings edits one catalog's own settings (its language, its content
 * rating, whichever server it reads from — whatever the site offers).
 */
export function SourceSettings({ moduleId, sourceId, title, onClose }: { moduleId: number; sourceId: string; title: string; onClose: () => void }) {
  const toast = useToast();
  const qc = useQueryClient();
  const key = ["prefs", moduleId, sourceId];
  const { data, isLoading, error } = useQuery({
    queryKey: key,
    queryFn: () => unwrap(api.GET("/api/v1/sources/{moduleId}/{sourceId}/preferences", { params: { path: { moduleId, sourceId } } })),
  });
  const set = async (position: number, type: string, value: unknown) => {
    try {
      await unwrap(api.PUT("/api/v1/sources/{moduleId}/{sourceId}/preferences", { params: { path: { moduleId, sourceId } }, body: { position, type, value } }));
      qc.invalidateQueries({ queryKey: key });
    } catch (e) {
      toast.fromError(e);
    }
  };
  return (
    <Modal open onClose={onClose} title={`${title} settings`}>
      {isLoading && <Loading />}
      {error && <ErrorBox error={error} />}
      {data?.length === 0 && <p className="text-sm text-muted">{t("This source has no settings.")}</p>}
      <div className="flex flex-col gap-4">
        {data
          ?.filter((p) => p.visible)
          .map((p) => (
            <div key={p.position} className="flex flex-col gap-1">
              <div className="text-sm font-medium">{p.title}</div>
              {p.summary && <div className="text-xs text-muted">{p.summary}</div>}
              {(p.type === "switch" || p.type === "checkbox") && <Switch checked={Boolean(p.value ?? p.defaultValue)} onChange={(v) => set(p.position, p.type, v)} />}
              {p.type === "list" && (
                <Select value={String(p.value ?? p.defaultValue ?? "")} onChange={(e) => set(p.position, p.type, e.target.value)}>
                  {p.entries?.map((label, i) => (
                    <option key={i} value={p.entryValues?.[i]}>
                      {label}
                    </option>
                  ))}
                </Select>
              )}
              {p.type === "edittext" && <Input defaultValue={String(p.value ?? "")} onBlur={(e) => set(p.position, p.type, e.target.value)} />}
              {p.type === "multiselect" && (
                <div className="flex flex-wrap gap-2">
                  {p.entries?.map((label, i) => {
                    const cur = (p.value as string[] | undefined) ?? [];
                    const v = p.entryValues?.[i] ?? label;
                    return (
                      <label key={i} className="flex items-center gap-1 text-sm">
                        <input type="checkbox" checked={cur.includes(v)} onChange={(e) => set(p.position, p.type, e.target.checked ? [...cur, v] : cur.filter((x) => x !== v))} />
                        {label}
                      </label>
                    );
                  })}
                </div>
              )}
            </div>
          ))}
      </div>
    </Modal>
  );
}
