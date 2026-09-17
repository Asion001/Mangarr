import { useEffect, useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowDown, ArrowUp, RotateCcw } from "lucide-react";
import { api, unwrap, type Catalog, type S } from "../../api/client";
import { useCatalogs, useRootFolders, useSeriesList } from "../../api/queries";
import { Badge, Button, Card, EmptyState, ErrorBox, IconButton, Loading, Select, Table, Td, Th } from "../../components/ui";
import { t } from "../../lib/i18n/core";
import { useToast } from "../../lib/toast";

type PriorityPreview = S["PriorityPreview"];

const catalogKey = (catalog: Catalog) => `${catalog.moduleId}:${catalog.id}`;

function move<T>(items: T[], index: number, direction: -1 | 1) {
  const target = index + direction;
  if (target < 0 || target >= items.length) return items;
  const next = [...items];
  [next[index], next[target]] = [next[target], next[index]];
  return next;
}

/** SourcePriorities edits inherited catalog order without enabling catalogs or linking series. */
export function SourcePriorities() {
  const toast = useToast();
  const qc = useQueryClient();
  const catalogs = useCatalogs();
  const roots = useRootFolders();
  const series = useSeriesList();
  const lists = useQuery({ queryKey: ["source-priorities"], queryFn: () => unwrap(api.GET("/api/v1/source-priorities")) });

  const languages = useMemo(
    () =>
      Array.from(
        new Set([
          ...(catalogs.data?.items ?? []).map((catalog) => catalog.lang),
          ...(roots.data ?? []).map((root) => root.language),
        ].filter((lang) => lang && lang !== "all" && lang !== "multi")),
      ).sort(),
    [catalogs.data, roots.data],
  );
  const scopes = useMemo(
    () => [
      ...(roots.data ?? []).map((root) => ({ value: `library:${root.id}`, label: `${t("Library")}: ${root.path}`, language: root.language })),
      ...languages.map((language) => ({ value: `language:${language}`, label: `${t("Language")}: ${language}`, language })),
    ],
    [languages, roots.data],
  );
  const [scope, setScope] = useState("");
  useEffect(() => {
    if (!scope && scopes[0]) setScope(scopes[0].value);
    else if (scope && !scopes.some((item) => item.value === scope)) setScope(scopes[0]?.value ?? "");
  }, [scope, scopes]);

  const selectedScope = scopes.find((item) => item.value === scope);
  const eligible = useMemo(() => {
    const language = selectedScope?.language;
    return (catalogs.data?.items ?? [])
      .filter((catalog) => !catalog.hidden && (!language || catalog.lang === language || catalog.lang === "all" || catalog.lang === "multi"))
      .sort((a, b) => a.priority - b.priority || a.displayName.localeCompare(b.displayName));
  }, [catalogs.data, selectedScope]);
  const byKey = useMemo(() => new Map(eligible.map((catalog) => [catalogKey(catalog), catalog])), [eligible]);
  const saved = useMemo(() => lists.data?.find((list) => list.scope === scope)?.sources ?? [], [lists.data, scope]);
  const resolved = useMemo(() => {
    const keys = saved.filter((key) => byKey.has(key));
    const seen = new Set(keys);
    for (const catalog of eligible) {
      const key = catalogKey(catalog);
      if (!seen.has(key)) keys.push(key);
    }
    return keys;
  }, [byKey, eligible, saved]);
  const [draft, setDraft] = useState<string[]>([]);
  useEffect(() => setDraft(resolved), [scope, resolved]);
  const dirty = draft.join("\n") !== resolved.join("\n");
  const [saving, setSaving] = useState(false);

  const save = async (sources: string[]) => {
    if (!scope) return;
    setSaving(true);
    try {
      await unwrap(api.PUT("/api/v1/source-priorities", { body: { scope, sources } }));
      await Promise.all([qc.invalidateQueries({ queryKey: ["source-priorities"] }), qc.invalidateQueries({ queryKey: ["catalogs"] })]);
      toast.success(t("Source priority saved"));
    } catch (error) {
      toast.fromError(error, t("Could not save source priority"));
    } finally {
      setSaving(false);
    }
  };

  const customSeries = (series.data ?? []).filter((item) => item.sourcePriorityMode === "custom");
  const migrationIDs = customSeries.slice(0, 200).map((item) => item.id);
  const [preview, setPreview] = useState<PriorityPreview[] | null>(null);
  const [migrating, setMigrating] = useState(false);
  const migrate = async (dryRun: boolean) => {
    if (!migrationIDs.length) return;
    setMigrating(true);
    try {
      const result = await unwrap(api.POST("/api/v1/source-priorities/inherit", { body: { seriesIds: migrationIDs, dryRun } }));
      setPreview(result);
      if (!dryRun) {
        await qc.invalidateQueries({ queryKey: ["series"] });
        toast.success(t("Series now inherit source priorities"));
      }
    } catch (error) {
      toast.fromError(error, t("Could not update series priorities"));
    } finally {
      setMigrating(false);
    }
  };

  if (catalogs.isLoading || roots.isLoading || lists.isLoading || series.isLoading) return <Loading />;
  const error = catalogs.error ?? roots.error ?? lists.error ?? series.error;
  if (error) return <ErrorBox error={error} />;
  if (!scopes.length) return <EmptyState title={t("No priority scopes")}>{t("Add a library or install a source first.")}</EmptyState>;

  return (
    <div className="flex flex-col gap-4">
      <Card
        title={t("Inherited source priority")}
        actions={
          <div className="flex gap-2">
            <Button size="sm" disabled={!saved.length || saving} icon={<RotateCcw className="size-3.5" />} onClick={() => save([])}>{t("Use global order")}</Button>
            <Button size="sm" variant="primary" disabled={!dirty || saving} loading={saving} onClick={() => save(draft)}>{t("Save order")}</Button>
          </div>
        }
      >
        <div className="mb-4 flex flex-wrap items-center gap-3">
          <Select className="max-w-xl" value={scope} onChange={(event) => setScope(event.target.value)}>
            {scopes.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}
          </Select>
          <span className="text-xs text-muted">{t("Library order overrides language order; sources not listed here keep their global catalog order.")}</span>
        </div>
        <div className="overflow-x-auto">
          <Table>
            <thead><tr><Th>{t("Order")}</Th><Th>{t("Source")}</Th><Th>{t("Language")}</Th><Th>{t("State")}</Th></tr></thead>
            <tbody>
              {draft.map((key, index) => {
                const catalog = byKey.get(key);
                if (!catalog) return null;
                return (
                  <tr key={key}>
                    <Td className="whitespace-nowrap">
                      <span className="mr-2 inline-block w-6 text-right text-muted">{index + 1}</span>
                      <IconButton title={t("Move up")} disabled={index === 0} onClick={() => setDraft((items) => move(items, index, -1))}><ArrowUp className="size-4" /></IconButton>
                      <IconButton title={t("Move down")} disabled={index === draft.length - 1} onClick={() => setDraft((items) => move(items, index, 1))}><ArrowDown className="size-4" /></IconButton>
                    </Td>
                    <Td><span className="font-medium">{catalog.displayName}</span><span className="ml-2 text-xs text-muted">{catalog.moduleName}</span></Td>
                    <Td><Badge>{catalog.lang}</Badge></Td>
                    <Td>{catalog.enabled ? <Badge tone="ok">{t("enabled")}</Badge> : <Badge>{t("disabled")}</Badge>}</Td>
                  </tr>
                );
              })}
            </tbody>
          </Table>
        </div>
      </Card>

      <Card title={t("Existing series")}>
        <p className="mb-3 text-sm text-muted">
          {t("Existing series keep their custom source order until you migrate them. Previewing and applying this change does not link sources or start downloads.")}
        </p>
        <div className="flex flex-wrap items-center gap-2">
          <Button disabled={!migrationIDs.length} loading={migrating} onClick={() => migrate(true)}>{t("Preview migration")} ({migrationIDs.length})</Button>
          {preview && migrationIDs.length > 0 && <Button variant="primary" loading={migrating} onClick={() => migrate(false)}>{t("Apply inheritance to previewed series")}</Button>}
          {!migrationIDs.length && <Badge tone="ok">{t("All series already inherit priorities")}</Badge>}
          {customSeries.length > 200 && <Badge tone="warn">{t("The first 200 series are shown per migration.")}</Badge>}
        </div>
        {preview && (
          <div className="mt-3 max-h-64 overflow-y-auto rounded border border-border">
            {preview.map((item) => (
              <div key={item.seriesId} className="flex items-center gap-3 border-b border-border px-3 py-2 text-sm last:border-b-0">
                <span className="min-w-0 flex-1 truncate">{item.title}</span>
                <span className="text-xs text-muted">{item.sources.map((source) => source.sourceName || source.sourceId).join(" → ") || t("No linked sources")}</span>
              </div>
            ))}
          </div>
        )}
      </Card>
    </div>
  );
}
