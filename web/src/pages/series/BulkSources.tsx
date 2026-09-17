import { t as tr, t } from "../../lib/i18n/core";
import { useMemo, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { api, unwrap, type S } from "../../api/client";
import { useCatalogs } from "../../api/queries";
import { Badge, Button, ErrorBox, Loading, Modal, Select, Table, Td, Th } from "../../components/ui";
import { useToast } from "../../lib/toast";

type Action = "add" | "remove" | "enable" | "disable";
type Result = S["BulkResult"];

const what: Record<Action, string> = {
  add: "Add as a fallback source",
  remove: "Remove this source",
  enable: "Switch this source on",
  disable: "Switch this source off",
};

/**
 * BulkSourcesModal adds one catalog to many series at once (or removes it, or
 * switches it off). Adding searches the catalog for each series, so it always
 * previews first and applies as a command.
 */
export function BulkSourcesModal({ ids, onClose }: { ids: number[]; onClose: () => void }) {
  const qc = useQueryClient();
  const toast = useToast();
  const { data: catalogs, isLoading } = useCatalogs();
  const [action, setAction] = useState<Action>("add");
  const [pick, setPick] = useState("");
  const [preview, setPreview] = useState<Result[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);

  const list = useMemo(
    () => [...(catalogs?.items ?? [])].sort((a, b) => a.displayName.localeCompare(b.displayName)),
    [catalogs],
  );
  const chosen = list.find((c) => `${c.moduleId}:${c.id}` === pick);

  const run = async (dryRun: boolean) => {
    if (!chosen) return;
    setBusy(true);
    setError(null);
    try {
      const r = await unwrap(
        api.POST("/api/v1/series/sources/bulk", {
          body: { action, moduleId: chosen.moduleId, sourceId: chosen.id, seriesIds: ids, dryRun },
        }),
      );
      if (dryRun) {
        setPreview(r.results ?? []);
        return;
      }
      qc.invalidateQueries({ queryKey: ["series"] });
      toast.success(`${chosen.displayName} queued for ${r.total} series`, tr("Watch it in Activity → Commands"));
      onClose();
    } catch (e) {
      setError(e);
    } finally {
      setBusy(false);
    }
  };

  const counted = (preview ?? []).reduce<Record<string, number>>((acc, r) => {
    acc[r.done] = (acc[r.done] ?? 0) + 1;
    return acc;
  }, {});

  return (
    <Modal
      open
      onClose={onClose}
      title={`Sources for ${ids.length} series`}
      footer={
        <>
          <Button onClick={onClose}>{t("Cancel")}</Button>
          <Button disabled={!chosen || busy} onClick={() => run(true)}>{t("Preview")}</Button>
          <Button variant="primary" disabled={!chosen || busy} onClick={() => run(false)}>{t("Apply")}</Button>
        </>
      }
    >
      {isLoading && <Loading />}
      <div className="flex flex-col gap-3">
        <div className="flex flex-wrap gap-2">
          <Select className="w-56" value={action} onChange={(e) => (setAction(e.target.value as Action), setPreview(null))}>
            {(Object.keys(what) as Action[]).map((a) => (
              <option key={a} value={a}>
                {what[a]}
              </option>
            ))}
          </Select>
          <Select className="w-56" value={pick} onChange={(e) => (setPick(e.target.value), setPreview(null))}>
            <option value="">{t("Pick a catalog…")}</option>
            {list.map((c) => (
              <option key={`${c.moduleId}:${c.id}`} value={`${c.moduleId}:${c.id}`}>
                {c.displayName} ({c.lang})
              </option>
            ))}
          </Select>
        </div>
        <p className="text-xs text-muted">
          {action === "add"
            ? tr("Each series is looked up at the catalog by title and linked last, so downloads keep preferring the sources it already has. A preview searches the first 25.")
            : tr("This only touches series already linked to the catalog.")}
        </p>
        {error ? <ErrorBox error={error} /> : null}
        {busy && <Loading />}
        {preview && (
          <>
            <div className="flex flex-wrap gap-2 text-sm">
              {Object.entries(counted).map(([k, n]) => (
                <Badge key={k} tone={k === "skipped" ? "warn" : "ok"}>
                  {n} {k}
                </Badge>
              ))}
              {preview.length === 0 && <span className="text-muted">{t("Nothing to do.")}</span>}
            </div>
            <div className="max-h-80 overflow-auto">
              <Table>
                <thead>
                  <tr>
                    <Th>{t("Series")}</Th>
                    <Th>{t("Found")}</Th>
                    <Th></Th>
                  </tr>
                </thead>
                <tbody>
                  {preview.map((r) => (
                    <tr key={r.seriesId} className={r.done === "skipped" ? "opacity-70" : undefined}>
                      <Td>{r.title}</Td>
                      <Td>{r.match || <span className="text-muted">{r.reason}</span>}</Td>
                      <Td className="text-right whitespace-nowrap">
                        {r.done !== "skipped" && <Badge tone="ok">{r.done}</Badge>}
                        {r.score ? <span className="ml-2 text-xs text-muted">{Math.round(r.score * 100)}%</span> : null}
                      </Td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            </div>
          </>
        )}
      </div>
    </Modal>
  );
}
