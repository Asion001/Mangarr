import { t as tr, t } from "../../lib/i18n/core";
import { useEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { api, unwrap, type ModuleResource, type S } from "../../api/client";
import { Badge, Button, ErrorBox, Loading, Modal, Select, Table, Td, Th } from "../../components/ui";
import { useToast } from "../../lib/toast";

type Row = S["SwitchRow"];

/**
 * SwitchEngine moves a library's source links from one source module to
 * another — off Suwayomi and onto mangarr's own sites, usually. Catalogs both
 * modules know by the same id move; the rest are named and left alone.
 */
export function SwitchEngine({ modules, from, onClose }: { modules: ModuleResource[]; from: ModuleResource; onClose: () => void }) {
  const qc = useQueryClient();
  const toast = useToast();
  const others = modules.filter((m) => m.id !== from.id);
  const [toId, setToId] = useState(others[0]?.id ?? 0);
  const [rows, setRows] = useState<Row[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);

  const plan = async (dryRun: boolean) => {
    if (!toId) return;
    setBusy(true);
    setError(null);
    try {
      const r = await unwrap(
        api.POST("/api/v1/series/sources/switch", { body: { fromModuleId: from.id, toModuleId: toId, dryRun } }),
      );
      setRows(r.rows);
      if (!dryRun) {
        qc.invalidateQueries({ queryKey: ["series"] });
        toast.success(tr("Switching in the background"), tr("Watch it in Activity → Commands"));
        onClose();
      }
    } catch (e) {
      setError(e);
    } finally {
      setBusy(false);
    }
  };

  useEffect(() => {
    setRows(null);
  }, [toId]);

  const moving = (rows ?? []).filter((r) => r.moves).reduce((n, r) => n + r.links, 0);
  return (
    <Modal
      open
      onClose={onClose}
      title={t("Switch engine")}
      footer={
        <>
          <Button onClick={onClose}>{t("Cancel")}</Button>
          <Button disabled={!toId || busy} onClick={() => plan(true)}>{t("Preview")}</Button>
          <Button variant="primary" disabled={!toId || busy || !moving} onClick={() => plan(false)}>{t("Move") + " "}{moving || ""}{" " + t("links")}</Button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <div className="flex flex-wrap items-center gap-2 text-sm">
          <span className="text-muted">{t("Move links from")}</span>
          <Badge>{from.name}</Badge>
          <span className="text-muted">{t("to")}</span>
          <Select className="w-48" value={toId} onChange={(e) => setToId(Number(e.target.value))}>
            {others.map((m) => (
              <option key={m.id} value={m.id}>
                {m.name}
              </option>
            ))}
          </Select>
        </div>
        <p className="text-xs text-muted">{t("A link moves when both modules know the catalog by the same id, which is the case wherever a site is built in under the id its extension has. Nothing is re-matched and no file is touched: the manga is identified by its catalog and its address, and the engine's own ids are dropped on the way.")}</p>
        {error ? <ErrorBox error={error} /> : null}
        {busy && <Loading />}
        {rows && (
          <div className="max-h-80 overflow-auto">
            <Table>
              <thead>
                <tr>
                  <Th>{t("Catalog")}</Th>
                  <Th>{t("Series")}</Th>
                  <Th></Th>
                </tr>
              </thead>
              <tbody>
                {rows.map((r) => (
                  <tr key={r.sourceId} className={r.moves ? undefined : "opacity-70"}>
                    <Td>{r.catalog}</Td>
                    <Td>{r.series}</Td>
                    <Td className="text-right">{r.moves ? <Badge tone="ok">{t("moves")}</Badge> : <span className="text-xs text-muted">{r.reason}</span>}</Td>
                  </tr>
                ))}
                {rows.length === 0 && (
                  <tr>
                    <Td colSpan={3} className="text-muted">{t("This module has no linked series.")}</Td>
                  </tr>
                )}
              </tbody>
            </Table>
          </div>
        )}
      </div>
    </Modal>
  );
}
