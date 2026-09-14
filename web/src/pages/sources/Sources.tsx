import { useMemo, useState } from "react";
import { Link, useNavigate } from "react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Download, RefreshCw, Settings2, Trash2, ArrowUpCircle, Plus } from "lucide-react";
import { api, apiUrl, unwrap, type Extension, type ModuleResource, type SourceInfo } from "../../api/client";
import { useModules, useSources } from "../../api/queries";
import { Cover } from "../../components/Cover";
import { Badge, Button, Card, EmptyState, ErrorBox, IconButton, Input, Loading, Modal, PageHeader, Select, Switch, Tabs } from "../../components/ui";
import { useToast } from "../../lib/toast";

export function SourcesPage() {
  const { data: modules, isLoading } = useModules("source");
  const [tab, setTab] = useState<"extensions" | "browse" | "stores">("extensions");
  const [moduleId, setModuleId] = useState<number>(0);
  const mods = modules ?? [];
  const current = mods.find((m) => m.id === moduleId) ?? mods[0];

  if (isLoading) return <Loading />;
  if (!mods.length)
    return (
      <>
        <PageHeader title="Sources" />
        <EmptyState title="No source module configured">
          Add a source module (for example Suwayomi with Keiyoushi extensions) in <Link to="/settings/sources" className="text-accent-2">Settings → Source modules</Link>.
        </EmptyState>
      </>
    );
  const caps = current?.capabilities ?? [];
  return (
    <>
      <PageHeader
        title="Sources"
        actions={
          mods.length > 1 && (
            <Select value={current?.id} onChange={(e) => setModuleId(Number(e.target.value))}>
              {mods.map((m) => (
                <option key={m.id} value={m.id}>
                  {m.name}
                </option>
              ))}
            </Select>
          )
        }
      />
      <Tabs
        value={tab}
        onChange={setTab}
        tabs={[
          ...(caps.includes("extensions") ? [{ value: "extensions" as const, label: "Extensions" }] : []),
          { value: "browse" as const, label: "Browse" },
          ...(caps.includes("extensions") ? [{ value: "stores" as const, label: "Stores" }] : []),
        ]}
      />
      {current && tab === "extensions" && caps.includes("extensions") && <Extensions module={current} />}
      {current && tab === "browse" && <Browse module={current} />}
      {current && tab === "stores" && <Stores module={current} />}
    </>
  );
}

function Extensions({ module }: { module: ModuleResource }) {
  const qc = useQueryClient();
  const toast = useToast();
  const [refresh, setRefresh] = useState(false);
  const { data, isFetching, error } = useQuery({
    queryKey: ["extensions", module.id, refresh],
    queryFn: () => unwrap(api.GET("/api/v1/modules/{id}/extensions", { params: { path: { id: module.id }, query: { refresh } } })),
  });
  const [q, setQ] = useState("");
  const [lang, setLang] = useState("");
  const [show, setShow] = useState<"all" | "installed" | "updates">("all");
  const [nsfw, setNsfw] = useState(false);
  const [busy, setBusy] = useState<string>("");

  const langs = useMemo(() => Array.from(new Set((data ?? []).map((e) => e.lang))).sort(), [data]);
  const list = useMemo(() => {
    let l = data ?? [];
    if (!nsfw) l = l.filter((e) => !e.nsfw);
    if (show === "installed") l = l.filter((e) => e.installed);
    if (show === "updates") l = l.filter((e) => e.hasUpdate);
    if (lang) l = l.filter((e) => e.lang === lang || e.lang === "all");
    if (q) l = l.filter((e) => e.name.toLowerCase().includes(q.toLowerCase()));
    return [...l].sort((a, b) => Number(b.installed) - Number(a.installed) || a.name.localeCompare(b.name));
  }, [data, q, lang, show, nsfw]);

  const act = async (e: Extension, action: "install" | "update" | "uninstall") => {
    setBusy(e.pkg);
    try {
      await unwrap(api.POST("/api/v1/modules/{id}/extensions/{pkg}/{action}", { params: { path: { id: module.id, pkg: e.pkg, action } } }));
      toast.success(`${e.name} ${action === "uninstall" ? "uninstalled" : action + "ed"}`);
      qc.invalidateQueries({ queryKey: ["extensions"] });
      qc.invalidateQueries({ queryKey: ["sources"] });
    } catch (err) {
      toast.fromError(err);
    } finally {
      setBusy("");
    }
  };

  return (
    <>
      <div className="mb-4 flex flex-wrap items-center gap-2">
        <Input className="max-w-xs" placeholder="Filter extensions…" value={q} onChange={(e) => setQ(e.target.value)} />
        <Select className="w-32" value={lang} onChange={(e) => setLang(e.target.value)}>
          <option value="">all langs</option>
          {langs.map((l) => (
            <option key={l}>{l}</option>
          ))}
        </Select>
        <Select className="w-40" value={show} onChange={(e) => setShow(e.target.value as typeof show)}>
          <option value="all">All</option>
          <option value="installed">Installed</option>
          <option value="updates">Updates</option>
        </Select>
        <Switch checked={nsfw} onChange={setNsfw} label="Show NSFW" />
        <Button className="ml-auto" icon={<RefreshCw className="size-4" />} loading={isFetching && refresh} onClick={() => (setRefresh(true), qc.invalidateQueries({ queryKey: ["extensions"] }))}>
          Refresh from stores
        </Button>
      </div>
      {isFetching && !data && <Loading />}
      {error && <ErrorBox error={error} />}
      <div className="grid gap-2 md:grid-cols-2 xl:grid-cols-3">
        {list.slice(0, 300).map((e) => (
          <div key={e.pkg} className="flex items-center gap-3 rounded-lg border border-border bg-panel p-2.5">
            <img src={e.iconUrl ? apiUrl(`api/v1/modules/${module.id}/asset`, { path: e.iconUrl }) : undefined} alt="" className="size-9 rounded bg-panel-2" loading="lazy" />
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-1.5">
                <span className="truncate font-medium">{e.name}</span>
                <Badge>{e.lang}</Badge>
                {e.nsfw && <Badge tone="err">18+</Badge>}
              </div>
              <div className="text-xs text-muted">
                v{e.versionName}
                {e.obsolete && <span className="ml-1 text-err">obsolete</span>}
              </div>
            </div>
            {e.installed ? (
              <div className="flex gap-1">
                {e.hasUpdate && (
                  <Button size="sm" variant="primary" loading={busy === e.pkg} icon={<ArrowUpCircle className="size-3.5" />} onClick={() => act(e, "update")}>
                    Update
                  </Button>
                )}
                <IconButton title="Uninstall" disabled={busy === e.pkg} onClick={() => act(e, "uninstall")}>
                  <Trash2 className="size-4" />
                </IconButton>
              </div>
            ) : (
              <Button size="sm" loading={busy === e.pkg} icon={<Download className="size-3.5" />} onClick={() => act(e, "install")}>
                Install
              </Button>
            )}
          </div>
        ))}
      </div>
      {list.length > 300 && <p className="mt-3 text-center text-sm text-muted">Showing 300 of {list.length}; refine the filter.</p>}
    </>
  );
}

function Browse({ module }: { module: ModuleResource }) {
  const { data: sources } = useSources();
  const nav = useNavigate();
  const mine = (sources ?? []).filter((s) => s.moduleId === module.id);
  const [sourceId, setSourceId] = useState("");
  const [type, setType] = useState<"popular" | "latest" | "search">("popular");
  const [q, setQ] = useState("");
  const [page, setPage] = useState(1);
  const [prefs, setPrefs] = useState<SourceInfo | null>(null);
  const src = mine.find((s) => s.id === sourceId) ?? mine[0];
  const { data, isFetching, error } = useQuery({
    queryKey: ["browse", module.id, src?.id, type, q, page],
    queryFn: () => unwrap(api.GET("/api/v1/sources/{moduleId}/{sourceId}/browse", { params: { path: { moduleId: module.id, sourceId: src!.id }, query: { type, q, page } } })),
    enabled: !!src && (type !== "search" || q.length > 0),
  });
  if (!mine.length) return <EmptyState title="No catalogs">Install an extension first.</EmptyState>;
  return (
    <>
      <div className="mb-4 flex flex-wrap items-center gap-2">
        <Select className="max-w-xs" value={src?.id} onChange={(e) => (setSourceId(e.target.value), setPage(1))}>
          {mine.map((s) => (
            <option key={s.id} value={s.id}>
              {s.displayName}
            </option>
          ))}
        </Select>
        <Select className="w-32" value={type} onChange={(e) => (setType(e.target.value as typeof type), setPage(1))}>
          <option value="popular">Popular</option>
          <option value="latest" disabled={!src?.supportsLatest}>
            Latest
          </option>
          <option value="search">Search</option>
        </Select>
        {type === "search" && (
          <form
            onSubmit={(e) => {
              e.preventDefault();
              setQ(new FormData(e.currentTarget).get("q") as string);
              setPage(1);
            }}
          >
            <Input name="q" placeholder="Search…" defaultValue={q} />
          </form>
        )}
        {module.capabilities.includes("preferences") && src && (
          <Button icon={<Settings2 className="size-4" />} onClick={() => setPrefs(src)}>
            Source settings
          </Button>
        )}
      </div>
      {isFetching && <Loading />}
      {error && <ErrorBox error={error} />}
      {data && (
        <>
          <div className="grid grid-cols-[repeat(auto-fill,minmax(130px,1fr))] gap-4">
            {data.mangas.map((m) => (
              <button key={m.url} className="group flex flex-col gap-1.5 text-left" onClick={() => nav(`/add?q=${encodeURIComponent(m.title)}`)}>
                <Cover
                  src={apiUrl(`api/v1/sources/${module.id}/${src!.id}/thumbnail`, { url: m.url, engineRef: m.engineRef })}
                  alt={m.title}
                  className="aspect-[2/3] w-full ring-accent/60 group-hover:ring-2"
                />
                <span className="line-clamp-2 text-xs">{m.title}</span>
              </button>
            ))}
          </div>
          <div className="mt-4 flex justify-center gap-2">
            <Button size="sm" disabled={page <= 1} onClick={() => setPage(page - 1)}>
              Previous
            </Button>
            <Button size="sm" disabled={!data.hasNext} onClick={() => setPage(page + 1)}>
              Next
            </Button>
          </div>
        </>
      )}
      {prefs && <PreferencesModal moduleId={module.id} source={prefs} onClose={() => setPrefs(null)} />}
    </>
  );
}

function PreferencesModal({ moduleId, source, onClose }: { moduleId: number; source: SourceInfo; onClose: () => void }) {
  const toast = useToast();
  const qc = useQueryClient();
  const key = ["prefs", moduleId, source.id];
  const { data, isLoading, error } = useQuery({
    queryKey: key,
    queryFn: () => unwrap(api.GET("/api/v1/sources/{moduleId}/{sourceId}/preferences", { params: { path: { moduleId, sourceId: source.id } } })),
  });
  const set = async (position: number, type: string, value: unknown) => {
    try {
      await unwrap(api.PUT("/api/v1/sources/{moduleId}/{sourceId}/preferences", { params: { path: { moduleId, sourceId: source.id } }, body: { position, type, value } }));
      qc.invalidateQueries({ queryKey: key });
    } catch (e) {
      toast.fromError(e);
    }
  };
  return (
    <Modal open onClose={onClose} title={`${source.displayName} settings`}>
      {isLoading && <Loading />}
      {error && <ErrorBox error={error} />}
      {data?.length === 0 && <p className="text-sm text-muted">This source has no settings.</p>}
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

function Stores({ module }: { module: ModuleResource }) {
  const qc = useQueryClient();
  const toast = useToast();
  const key = ["stores", module.id];
  const { data, isLoading, error } = useQuery({ queryKey: key, queryFn: () => unwrap(api.GET("/api/v1/modules/{id}/stores", { params: { path: { id: module.id } } })) });
  const [url, setUrl] = useState("");
  const add = async () => {
    try {
      await unwrap(api.POST("/api/v1/modules/{id}/stores", { params: { path: { id: module.id } }, body: { url } }));
      setUrl("");
      qc.invalidateQueries({ queryKey: key });
      qc.invalidateQueries({ queryKey: ["extensions"] });
    } catch (e) {
      toast.fromError(e);
    }
  };
  const remove = async (u: string) => {
    try {
      await unwrap(api.DELETE("/api/v1/modules/{id}/stores", { params: { path: { id: module.id }, query: { url: u } } }));
      qc.invalidateQueries({ queryKey: key });
    } catch (e) {
      toast.fromError(e);
    }
  };
  return (
    <Card title="Extension stores">
      {isLoading && <Loading />}
      {error && <ErrorBox error={error} />}
      <div className="flex flex-col gap-2">
        {data?.map((u) => (
          <div key={u} className="flex items-center gap-2 rounded bg-panel-2 px-3 py-2 text-sm">
            <span className="flex-1 truncate font-mono text-xs">{u}</span>
            <IconButton title="Remove" onClick={() => remove(u)}>
              <Trash2 className="size-4" />
            </IconButton>
          </div>
        ))}
        <div className="mt-2 flex gap-2">
          <Input value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://…/index.min.json" />
          <Button icon={<Plus className="size-4" />} disabled={!url} onClick={add}>
            Add store
          </Button>
        </div>
        <p className="text-xs text-muted">Keiyoushi is added by default. Only add stores you trust: extensions run as code inside the engine.</p>
      </div>
    </Card>
  );
}
