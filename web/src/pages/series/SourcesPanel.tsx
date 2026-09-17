import { t } from "../../lib/i18n/core";
import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { ArrowDown, ArrowUp, ExternalLink, Plus, Trash2 } from "lucide-react";
import { api, unwrap, type Series, type SeriesSource } from "../../api/client";
import { Badge, Button, Card, Confirm, IconButton, Switch, Table, Td, Th } from "../../components/ui";
import { relative } from "../../lib/format";
import { useToast } from "../../lib/toast";
import { SourceSearchModal } from "./SourceSearch";

export function SourcesPanel({ series }: { series: Series }) {
  const qc = useQueryClient();
  const toast = useToast();
  const [adding, setAdding] = useState(false);
  const [removing, setRemoving] = useState<SeriesSource | null>(null);
  const sources = [...(series.sources ?? [])].sort((a, b) => a.priority - b.priority || a.id - b.id);

  const update = async (ss: SeriesSource, body: { priority?: number; enabled?: boolean }) => {
    try {
      await unwrap(api.PUT("/api/v1/series/{id}/sources/{linkId}", { params: { path: { id: series.id, linkId: ss.id } }, body }));
      qc.invalidateQueries({ queryKey: ["series", series.id] });
    } catch (e) {
      toast.fromError(e);
    }
  };

  const move = async (i: number, dir: -1 | 1) => {
    const j = i + dir;
    if (j < 0 || j >= sources.length) return;
    const reordered = [...sources];
    [reordered[i], reordered[j]] = [reordered[j], reordered[i]];
    for (let k = 0; k < reordered.length; k++) {
      if (reordered[k].priority !== k) await update(reordered[k], { priority: k });
    }
  };

  const remove = async () => {
    if (!removing) return;
    try {
      await unwrap(api.DELETE("/api/v1/series/{id}/sources/{linkId}", { params: { path: { id: series.id, linkId: removing.id } } }));
      qc.invalidateQueries({ queryKey: ["series", series.id] });
      setRemoving(null);
    } catch (e) {
      toast.fromError(e);
    }
  };

  return (
    <Card
      title={`Sources (${sources.length})`}
      className="mb-6"
      actions={
        <Button size="sm" icon={<Plus className="size-3.5" />} onClick={() => setAdding(true)}>{t("Link source")}</Button>
      }
    >
      {sources.length === 0 ? (
        <p className="text-sm text-muted">{t("No source is linked. Link one to receive chapters.")}</p>
      ) : (
        <Table className="border-0">
          <thead>
            <tr>
              <Th>{t("Priority")}</Th>
              <Th>{t("Source")}</Th>
              <Th>{t("Title at source")}</Th>
              <Th>{t("Last check")}</Th>
              <Th>{t("Next check")}</Th>
              <Th>{t("Status")}</Th>
              <Th>{t("Enabled")}</Th>
              <Th />
            </tr>
          </thead>
          <tbody>
            {sources.map((ss, i) => (
              <tr key={ss.id}>
                <Td>
                  <div className="flex items-center gap-0.5">
                    <span className="w-5 text-center text-muted">{i + 1}</span>
                    <IconButton title={t("Higher priority")} disabled={i === 0} onClick={() => move(i, -1)}>
                      <ArrowUp className="size-3.5" />
                    </IconButton>
                    <IconButton title={t("Lower priority")} disabled={i === sources.length - 1} onClick={() => move(i, 1)}>
                      <ArrowDown className="size-3.5" />
                    </IconButton>
                  </div>
                </Td>
                <Td className="font-medium">{ss.sourceName || ss.sourceId}</Td>
                <Td>
                  {ss.webUrl ? (
                    <a href={ss.webUrl} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 hover:text-accent-2">
                      {ss.title || ss.mangaUrl} <ExternalLink className="size-3" />
                    </a>
                  ) : (
                    ss.title || ss.mangaUrl
                  )}
                </Td>
                <Td className="text-muted">{relative(ss.lastCheckedAt)}</Td>
                <Td className="text-muted">{ss.enabled ? relative(ss.nextCheckAt) : "—"}</Td>
                <Td>
                  {ss.consecutiveFailures > 0 ? (
                    <Badge tone="err" title={ss.lastError}>
                      {ss.consecutiveFailures}{" " + t("failures")}</Badge>
                  ) : ss.lastSuccessAt ? (
                    <Badge tone="ok">{t("ok")}</Badge>
                  ) : (
                    <Badge>{t("pending")}</Badge>
                  )}
                </Td>
                <Td>
                  <Switch checked={ss.enabled} onChange={(v) => update(ss, { enabled: v })} />
                </Td>
                <Td className="text-right">
                  <IconButton title={t("Unlink")} onClick={() => setRemoving(ss)}>
                    <Trash2 className="size-4" />
                  </IconButton>
                </Td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      {sources.some((s) => s.lastError) && (
        <div className="mt-3 flex flex-col gap-1">
          {sources
            .filter((s) => s.lastError)
            .map((s) => (
              <p key={s.id} className="text-xs text-err">
                {s.sourceName}: {s.lastError}
              </p>
            ))}
        </div>
      )}
      {adding && (
        <SourceSearchModal
          initialQuery={series.title}
          titles={[series.title, ...(series.metadata?.altTitles ?? [])]}
          title={t("Link a source")}
          onClose={() => setAdding(false)}
          onPick={async (m, g) => {
            try {
              await unwrap(
                api.POST("/api/v1/series/{id}/sources", {
                  params: { path: { id: series.id } },
                  body: { moduleId: g.moduleId, sourceId: g.sourceId, url: m.url, engineRef: m.engineRef, title: m.title, sourceName: g.sourceName, lang: g.lang },
                }),
              );
              toast.success(`Linked ${g.sourceName}`);
              qc.invalidateQueries({ queryKey: ["series", series.id] });
              setAdding(false);
            } catch (e) {
              toast.fromError(e);
            }
          }}
        />
      )}
      <Confirm
        open={!!removing}
        title={t("Unlink source")}
        danger
        confirmLabel={t("Unlink")}
        message={`Stop using ${removing?.sourceName} for this series? Downloaded files are kept.`}
        onConfirm={remove}
        onClose={() => setRemoving(null)}
      />
    </Card>
  );
}
