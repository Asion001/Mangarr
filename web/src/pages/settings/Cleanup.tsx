import { t } from "../../lib/i18n/core";
import { Link } from "react-router";
import { useQuery } from "@tanstack/react-query";
import { Eraser, RefreshCw } from "lucide-react";
import { api, unwrap, type S } from "../../api/client";
import { usePushCommand, useReaders, useTags } from "../../api/queries";
import { Badge, Button, Card, ErrorBox, Field, Input, Loading, PageHeader, SaveBar, Switch, Table, Td, Th } from "../../components/ui";
import { bytes, relative } from "../../lib/format";
import { useSettingsDoc } from "./useSettingsDoc";

type CleanupSettings = S["Cleanup"];

export function CleanupPage() {
  const { value: c, patch, save, saving, isLoading, error, lock, dirty, reset } = useSettingsDoc<CleanupSettings>("cleanup");
  const { data: readers } = useReaders();
  const { data: tags } = useTags();
  const push = usePushCommand();
  const preview = useQuery({ queryKey: ["cleanup-preview"], queryFn: () => unwrap(api.GET("/api/v1/cleanup/preview")) });
  const toggleStatus = (s: string) => {
    if (!c) return;
    const cur = c.statuses ?? [];
    patch({ statuses: cur.includes(s) ? cur.filter((x) => x !== s) : [...cur, s] });
  };
  const toggleReader = (id: number) => {
    if (!c) return;
    const cur = c.readerIds ?? [];
    patch({ readerIds: cur.includes(id) ? cur.filter((x) => x !== id) : [...cur, id] });
  };
  return (
    <>
      <PageHeader
        title={t("Read-based cleanup")}
        subtitle={t("Delete chapters every reader has finished to save space. Cleaned chapters are never downloaded again unless you restore them.")}
      />
      {isLoading && <Loading />}
      {error && <ErrorBox error={error} />}
      {c && (
        <Card title={t("Rules")} className="mb-6">
          <div className="grid gap-5 md:grid-cols-2">
            <div className="flex flex-col gap-3">
              <Switch env={lock("enabled")} checked={c.enabled} onChange={(v) => patch({ enabled: v })} label={<b>{t("Enable cleanup")}</b>} />
              <Switch env={lock("dryRun")} checked={c.dryRun} onChange={(v) => patch({ dryRun: v })} label={t("Dry run (only preview, never delete)")} />
              <Switch env={lock("ignoreReadersNotStarted")} checked={c.ignoreReadersNotStarted} onChange={(v) => patch({ ignoreReadersNotStarted: v })} label={t("Ignore readers who never started a series")} />
              <Switch env={lock("useRecycleBin")} checked={c.useRecycleBin} onChange={(v) => patch({ useRecycleBin: v })} label={t("Move to recycle bin instead of deleting")} />
            </div>
            <div className="grid grid-cols-2 gap-4">
              <Field env={lock("keepLastRead")} label={t("Keep last read chapters")} help={t("Keeps apps' progress anchored")}>
                <Input type="number" min={0} value={c.keepLastRead} onChange={(e) => patch({ keepLastRead: Number(e.target.value) })} />
              </Field>
              <Field env={lock("graceDays")} label={t("Grace period (days)")} help={t("After the last reader finished")}>
                <Input type="number" min={0} value={c.graceDays} onChange={(e) => patch({ graceDays: Number(e.target.value) })} />
              </Field>
              <Field env={lock("minFreeSpaceGb")} label={t("Only when free space below (GB)")} help={t("0 = always")}>
                <Input type="number" min={0} value={c.minFreeSpaceGb} onChange={(e) => patch({ minFreeSpaceGb: Number(e.target.value) })} />
              </Field>
            </div>
            <Field env={lock("statuses")} label={t("Series status in scope")}>
              <div className="flex flex-wrap gap-3">
                {["ongoing", "completed", "hiatus", "cancelled", "unknown"].map((s) => (
                  <label key={s} className="flex items-center gap-1.5 text-sm">
                    <input type="checkbox" checked={(c.statuses ?? []).includes(s)} onChange={() => toggleStatus(s)} /> {s}
                  </label>
                ))}
              </div>
            </Field>
            <Field env={lock("readerIds")} label={t("Required readers")} help={t("None selected = every reader counting for cleanup.")}>
              <div className="flex flex-wrap gap-3">
                {readers?.map((r) => (
                  <label key={r.id} className="flex items-center gap-1.5 text-sm">
                    <input type="checkbox" checked={(c.readerIds ?? []).includes(r.id)} onChange={() => toggleReader(r.id)} /> {r.name}
                  </label>
                ))}
                {!readers?.length && (
                  <span className="text-sm text-muted">{t("No readers yet —") + " "}<Link to="/settings/readers" className="text-accent-2">{t("add readers")}</Link>.
                  </span>
                )}
              </div>
            </Field>
            <Field env={lock("excludeTags")} label={t("Excluded tags")} help={t("Series with any of these tags are never cleaned.")}>
              <div className="flex flex-wrap gap-3">
                {Array.from(new Set([...(c.excludeTags ?? []), ...(tags ?? []).map((t) => t.label)])).map((t) => (
                  <label key={t} className="flex items-center gap-1.5 text-sm">
                    <input
                      type="checkbox"
                      checked={(c.excludeTags ?? []).includes(t)}
                      onChange={(e) => patch({ excludeTags: e.target.checked ? [...(c.excludeTags ?? []), t] : (c.excludeTags ?? []).filter((x) => x !== t) })}
                    />
                    {t}
                  </label>
                ))}
              </div>
            </Field>
          </div>
        </Card>
      )}
      <Card
        title={t("Preview")}
        actions={
          <>
            <Button size="sm" icon={<RefreshCw className="size-3.5" />} onClick={() => preview.refetch()}>{t("Refresh")}</Button>
            <Button
              size="sm"
              variant="danger"
              icon={<Eraser className="size-3.5" />}
              disabled={!preview.data?.candidates.length}
              onClick={() => push.mutate({ name: "Cleanup", body: { force: true }, label: "Cleanup started" })}
            >{t("Run now")}</Button>
          </>
        }
      >
        {preview.isLoading && <Loading />}
        {preview.error && <ErrorBox error={preview.error} />}
        {preview.data && (
          <>
            <p className="mb-3 text-sm">
              {preview.data.candidates.length}{" " + t("chapters ·") + " "}<b>{bytes(preview.data.totalSize)}</b>{" " + t("would be freed")}{!preview.data.enabled && <span className="ml-2"><Badge tone="warn">{t("cleanup disabled")}</Badge></span>}
              {preview.data.dryRun && <span className="ml-2"><Badge tone="info">{t("dry run")}</Badge></span>}
            </p>
            {preview.data.candidates.length > 0 && (
              <Table className="mb-4">
                <thead>
                  <tr>
                    <Th>{t("Series")}</Th>
                    <Th>{t("Chapter")}</Th>
                    <Th>{t("Size")}</Th>
                    <Th>{t("Last read")}</Th>
                  </tr>
                </thead>
                <tbody>
                  {preview.data.candidates.slice(0, 200).map((cd) => (
                    <tr key={cd.chapterId}>
                      <Td>
                        <Link to={`/series/${cd.seriesId}`} className="hover:text-accent-2">
                          {cd.seriesTitle}
                        </Link>
                      </Td>
                      <Td>{cd.chapter}</Td>
                      <Td>{bytes(cd.size)}</Td>
                      <Td className="text-muted">{relative(cd.lastReadAt)}</Td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            )}
            {preview.data.skipped.length > 0 && (
              <details className="text-sm">
                <summary className="cursor-pointer text-muted">{preview.data.skipped.length}{" " + t("series skipped")}</summary>
                <ul className="mt-2 flex flex-col gap-1">
                  {preview.data.skipped.map((s) => (
                    <li key={s.seriesId} className="text-xs">
                      <span className="text-fg">{s.seriesTitle}</span> <span className="text-muted">— {s.reason}</span>
                    </li>
                  ))}
                </ul>
              </details>
            )}
          </>
        )}
      </Card>
      <SaveBar dirty={dirty} saving={saving} onSave={async () => (await save(), preview.refetch())} onDiscard={reset} />
    </>
  );
}
