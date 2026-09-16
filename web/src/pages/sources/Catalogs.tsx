import { useMemo } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { ArrowDown, ArrowUp, TimerReset } from "lucide-react";
import { api, apiUrl, unwrap, type Catalog, type ModuleResource, type S } from "../../api/client";
import { useCatalogs } from "../../api/queries";
import { Badge, Button, Card, EmptyState, ErrorBox, IconButton, Input, Loading, Select, Switch, Table, Td, Th } from "../../components/ui";
import { useToast } from "../../lib/toast";
import { useListParam, useQueryParam } from "../../lib/urlState";
import { useSettingsDoc } from "../settings/useSettingsDoc";

type Patch = { enabled?: boolean; priority?: number; throttle?: S["ThrottleConfig"]; clearCooldown?: boolean };
const key = (c: Catalog) => `${c.moduleId}:${c.id}`;

/** Catalogs manages which catalogs are searched, their order and throttling. */
export function Catalogs({ module }: { module: ModuleResource }) {
  const qc = useQueryClient();
  const toast = useToast();
  const { data, isLoading, error } = useCatalogs();
  const src = useSettingsDoc<S["Sources"]>("sources");
  const [q, setQ] = useQueryParam("catalog", "");
  const [lang, setLang] = useListParam("lang", "");

  const mine = useMemo(() => (data?.items ?? []).filter((c) => c.moduleId === module.id), [data, module.id]);
  const langs = useMemo(() => Array.from(new Set(mine.map((c) => c.lang))).sort(), [mine]);
  const list = useMemo(() => {
    let l = mine;
    if (lang) l = l.filter((c) => c.lang === lang);
    if (q) l = l.filter((c) => c.displayName.toLowerCase().includes(q.toLowerCase()));
    return [...l].sort((a, b) => a.priority - b.priority || a.lang.localeCompare(b.lang) || a.name.localeCompare(b.name));
  }, [mine, q, lang]);

  const update = async (patches: Record<string, Patch>) => {
    try {
      await unwrap(api.PUT("/api/v1/catalogs", { body: patches }));
      qc.invalidateQueries({ queryKey: ["catalogs"] });
      qc.invalidateQueries({ queryKey: ["sources"] });
    } catch (e) {
      toast.fromError(e, "Could not update catalogs");
    }
  };

  // move a catalog within the visible list; renumbers the list 10, 20, 30…
  const move = (i: number, dir: -1 | 1) => {
    const j = i + dir;
    if (j < 0 || j >= list.length) return;
    const order = [...list];
    [order[i], order[j]] = [order[j], order[i]];
    const patches: Record<string, Patch> = {};
    order.forEach((c, n) => {
      const p = (n + 1) * 10;
      if (c.priority !== p) patches[key(c)] = { priority: p };
    });
    update(patches);
  };

  const s = src.value;
  const setSources = (p: Partial<S["Sources"]>) => {
    if (!s) return;
    const v = { ...s, ...p };
    src.setValue(v);
    src.save(v);
  };

  if (isLoading) return <Loading />;
  if (error) return <ErrorBox error={error} />;
  if (!mine.length) return <EmptyState title="No catalogs">Install an extension first.</EmptyState>;
  return (
    <>
      {s && (
        <Card title="Defaults" className="mb-4">
          <div className="flex flex-col gap-3">
            <Switch
              checked={s.hideNsfw}
              env={src.lock("hideNsfw")}
              onChange={(v) => setSources({ hideNsfw: v })}
              label="Hide NSFW catalogs everywhere (search, browse, add series)"
            />
            <div className="flex flex-wrap items-center gap-1.5 text-sm">
              <span className="mr-1 text-muted">Search languages by default:</span>
              {langs.map((l) => {
                const on = (s.defaultLanguages ?? []).includes(l);
                return (
                  <button
                    key={l}
                    disabled={!!src.lock("defaultLanguages")}
                    onClick={() => setSources({ defaultLanguages: on ? s.defaultLanguages.filter((x) => x !== l) : [...(s.defaultLanguages ?? []), l] })}
                    className={`rounded border px-2 py-0.5 text-xs ${on ? "border-accent bg-accent/15 text-fg" : "border-border text-muted hover:text-fg"}`}
                  >
                    {l}
                  </button>
                );
              })}
              {!(s.defaultLanguages ?? []).length && <span className="text-xs text-muted">(none selected = all languages)</span>}
            </div>
          </div>
        </Card>
      )}
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <Input className="max-w-xs" placeholder="Filter catalogs…" value={q} onChange={(e) => setQ(e.target.value)} />
        <Select className="w-32" value={lang} onChange={(e) => setLang(e.target.value)}>
          <option value="">all langs</option>
          {langs.map((l) => (
            <option key={l}>{l}</option>
          ))}
        </Select>
        <div className="ml-auto flex gap-2">
          <Button size="sm" onClick={() => update(Object.fromEntries(list.map((c) => [key(c), { enabled: true }])))}>
            Enable shown
          </Button>
          <Button size="sm" onClick={() => update(Object.fromEntries(list.map((c) => [key(c), { enabled: false }])))}>
            Disable shown
          </Button>
        </div>
      </div>
      <p className="mb-3 text-xs text-muted">
        Searches go through enabled catalogs from the top down. Disabled catalogs are still searchable with “All sources”; hidden NSFW catalogs never are.
      </p>
      <div className="overflow-x-auto">
        <Table>
          <thead>
            <tr>
              <Th>Order</Th>
              <Th>Catalog</Th>
              <Th>Enabled</Th>
              <Th>Throttling</Th>
              <Th></Th>
            </tr>
          </thead>
          <tbody>
            {list.map((c, i) => (
              <tr key={key(c)} className={c.hidden || !c.enabled ? "opacity-60" : undefined}>
                <Td className="whitespace-nowrap">
                  <IconButton title="Move up" disabled={i === 0} onClick={() => move(i, -1)}>
                    <ArrowUp className="size-4" />
                  </IconButton>
                  <IconButton title="Move down" disabled={i === list.length - 1} onClick={() => move(i, 1)}>
                    <ArrowDown className="size-4" />
                  </IconButton>
                </Td>
                <Td>
                  <div className="flex items-center gap-2">
                    {c.iconUrl && <img src={apiUrl(`api/v1/modules/${module.id}/asset`, { path: c.iconUrl })} alt="" className="size-6 rounded" loading="lazy" />}
                    <span className="font-medium">{c.displayName}</span>
                    {c.nsfw && <Badge tone={c.hidden ? "err" : "warn"}>{c.hidden ? "18+ hidden" : "18+"}</Badge>}
                    {c.cooldownUntil && (
                      <Badge tone="warn" title={c.cooldownReason}>
                        paused until {new Date(c.cooldownUntil).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}
                      </Badge>
                    )}
                  </div>
                </Td>
                <Td>
                  <Switch checked={c.enabled} disabled={c.hidden} onChange={(v) => update({ [key(c)]: { enabled: v } })} />
                </Td>
                <Td>
                  <Select
                    className="w-32"
                    value={c.throttle?.preset ?? ""}
                    onChange={(e) => update({ [key(c)]: { throttle: { ...c.throttle, preset: e.target.value as S["ThrottleConfig"]["preset"] } } })}
                  >
                    <option value="">default</option>
                    <option value="gentle">gentle</option>
                    <option value="normal">normal</option>
                    <option value="fast">fast</option>
                  </Select>
                </Td>
                <Td className="text-right">
                  {c.cooldownUntil && (
                    <IconButton title="Resume now" onClick={() => update({ [key(c)]: { clearCooldown: true } })}>
                      <TimerReset className="size-4" />
                    </IconButton>
                  )}
                </Td>
              </tr>
            ))}
          </tbody>
        </Table>
      </div>
    </>
  );
}
