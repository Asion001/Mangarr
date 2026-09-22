import { t as tr, t } from "../../lib/i18n/core";
import { useEffect, useState, type ReactNode } from "react";
import { Link, useLocation, useNavigate, useParams, useSearchParams } from "react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowDown, ArrowUp, BookPlus, Search, X } from "lucide-react";
import { api, unwrap, type AddRequest, type LookupResult, type S } from "../../api/client";
import { useCatalogs, useProfiles, useRootFolders } from "../../api/queries";
import { Cover } from "../../components/Cover";
import { Badge, Button, Card, ErrorBox, Field, IconButton, Input, Loading, PageHeader, Select, Switch } from "../../components/ui";
import { sessionState, useQueryParam } from "../../lib/urlState";
import { useToast } from "../../lib/toast";
import { useAccount } from "../../lib/account";
import { useSettingsDoc } from "../settings/useSettingsDoc";
import { SourceSearch, pickKey, type Picked, type Scope } from "./SourceSearch";

/** MetadataSearch looks up series across metadata modules. */
export function MetadataSearch({
  initialQuery = "",
  query: controlled,
  setQuery: setControlled,
  onPick,
  action,
  placeholder = "Search by title (AniList and other metadata modules)",
  language = "",
}: {
  initialQuery?: string;
  query?: string;
  setQuery?: (q: string) => void;
  onPick?: (c: LookupResult) => void;
  /** action replaces the Select button (e.g. Request). */
  action?: (c: LookupResult) => ReactNode;
  placeholder?: string;
  language?: string;
}) {
  const { isAdmin } = useAccount();
  const [local, setLocal] = useState(initialQuery);
  const query = controlled ?? local;
  const setQuery = setControlled ?? setLocal;
  const [draft, setDraft] = useState(query);
  useEffect(() => setDraft(query), [query]);
  const { data, isFetching, error } = useQuery({
    queryKey: ["lookup", query, language],
    queryFn: () => unwrap(api.GET("/api/v1/series/lookup", { params: { query: { q: query, lang: language || undefined } } })),
    enabled: query.length > 0,
    staleTime: 5 * 60_000,
  });
  return (
    <div>
      <form
        className="mb-4 flex gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          setQuery(draft.trim());
        }}
      >
        <Input autoFocus value={draft} onChange={(e) => setDraft(e.target.value)} placeholder={placeholder} />
        <Button type="submit" variant="primary" icon={<Search className="size-4" />}>{t("Search")}</Button>
      </form>
      {isFetching && <Loading />}
      {error && <ErrorBox error={error} />}
      {data?.errors?.map((e) => (
        <p key={e} className="mb-2 text-xs text-warn">
          {e}
        </p>
      ))}
      <div className="flex flex-col gap-2">
        {data?.results.map((r) => (
          <div key={r.provider + r.id} className="flex gap-3 rounded-lg border border-border bg-panel p-3">
            <Cover src={r.coverUrl} alt={r.title} className="aspect-[2/3] w-16 shrink-0" />
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-2">
                <span className="font-medium">{r.title}</span>
                {r.year ? <Badge>{r.year}</Badge> : null}
                {r.format && <Badge>{r.format}</Badge>}
                {r.status && <Badge tone="info">{r.status}</Badge>}
                <Badge tone="accent">{r.moduleName}</Badge>
                {r.also?.map((a) => (
                  <Badge key={a.provider}>{a.provider}</Badge>
                ))}
              </div>
              {r.altTitles && <p className="mt-0.5 line-clamp-1 text-xs text-muted">{r.altTitles.slice(0, 3).join(" · ")}</p>}
              {r.description && <p className="mt-1 line-clamp-2 text-sm text-fg/80">{r.description}</p>}
            </div>
            <div className="shrink-0 self-center">
              {action ? (
                action(r)
              ) : r.existingSeriesId ? (
                <Link to={`/series/${r.existingSeriesId}`}>
                  <Button size="sm">{t("In library")}</Button>
                </Link>
              ) : (
                <Button size="sm" variant="primary" onClick={() => onPick?.(r)}>{t("Select")}</Button>
              )}
            </div>
          </div>
        ))}
        {data && data.results.length === 0 && data.providers > 0 && <p className="text-sm text-muted">{t("No metadata found.")}</p>}
        {data && data.providers === 0 && (
          <p className="text-sm text-warn">
            {t("No metadata provider is set up, so nothing was searched.")}{" "}
            {isAdmin ? <Link to="/settings/metadata" className="text-accent-2 hover:underline">{t("Add one in Settings → Metadata")}</Link> : t("Ask an admin to add one.")}
          </p>
        )}
      </div>
    </div>
  );
}

const steps = ["Metadata", "Sources", "Options"] as const;

function StepHeader({ step, title, base, search }: { step: 0 | 1 | 2; title?: string; base?: string; search?: string }) {
  const links = ["/add", base ? `${base}/sources${search ?? ""}` : undefined, base ? `${base}/options${search ?? ""}` : undefined];
  return (
    <PageHeader
      title={title ? `Add ${title}` : tr("Add series")}
      subtitle={
        <span className="flex flex-wrap gap-2">
          {steps.map((s, i) => (
            <span key={s} className={i === step ? "font-medium text-fg" : ""}>
              {i < step && links[i] ? <Link to={links[i]!}>{`${i + 1}. ${s}`}</Link> : `${i + 1}. ${s}`}
              {i < steps.length - 1 && <span className="ml-2 text-muted">›</span>}
            </span>
          ))}
        </span>
      }
    />
  );
}

/** Step 1 (/add?q=): pick metadata or continue with a title only. */
export function AddSearchStep() {
  const nav = useNavigate();
  const qc = useQueryClient();
  const [q, setQ] = useQueryParam("q");
  const [lang, setLang] = useQueryParam("lang");
  const { data: catalogs } = useCatalogs();
  const languages = Array.from(new Set(["en", "ru", ...(catalogs?.items ?? []).map((c) => c.lang).filter((v) => v !== "all" && v !== "multi")])).sort();
  const [title, setTitle] = useState(q);
  const pick = (r: LookupResult) => {
    qc.setQueryData(["lookup-item", r.moduleId, r.id], r);
    nav(`/add/${r.moduleId}/${encodeURIComponent(r.id)}/sources${lang ? `?lang=${encodeURIComponent(lang)}` : ""}`);
  };
  return (
    <>
      <StepHeader step={0} />
      <Card>
        <div className="mb-3 flex items-center gap-2">
          <span className="text-sm text-muted">{t("Language")}</span>
          <Select className="w-40" value={lang} onChange={(e) => setLang(e.target.value, { replace: false })}>
            <option value="">{t("Any language")}</option>
            {languages.map((value) => <option key={value} value={value}>{value}</option>)}
          </Select>
        </div>
        <MetadataSearch query={q} language={lang} setQuery={(v) => setQ(v, { replace: false })} onPick={pick} />
        <div className="mt-4 border-t border-border pt-4">
          <p className="mb-2 text-sm text-muted">{t("Not on any metadata site? Add it using only the source's information.")}</p>
          <form
            className="flex gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              if (title.trim()) nav(`/add/manual/-/sources?title=${encodeURIComponent(title.trim())}${lang ? `&lang=${encodeURIComponent(lang)}` : ""}`);
            }}
          >
            <Input value={title} onChange={(e) => setTitle(e.target.value)} placeholder={t("Series title")} />
            <Button type="submit">{t("Continue without metadata")}</Button>
          </form>
        </div>
      </Card>
      <p className="mt-3 text-sm text-muted">{t("Coming from Mihon, Tachiyomi, Suwayomi or Aidoku?")}{" "}
        <Link to="/import" className="text-accent-2 hover:underline">{t("Import your library from a backup")}</Link>
      </p>
    </>
  );
}

/** useAddContext resolves the series being added from the route. */
function useAddContext() {
  const { moduleId = "", metaId = "" } = useParams();
  const [params] = useSearchParams();
  const manual = moduleId === "manual";
  const meta = useQuery({
    queryKey: ["lookup-item", Number(moduleId), metaId, params.get("lang") ?? ""],
    queryFn: () => unwrap(api.GET("/api/v1/series/lookup/{moduleId}/{id}", { params: { path: { moduleId: Number(moduleId), id: metaId }, query: { lang: params.get("lang") || undefined } } })),
    enabled: !manual && !!metaId,
    staleTime: 30 * 60_000,
  });
  const title = manual ? (params.get("title") ?? "") : (meta.data?.title ?? "");
  const base = `/add/${moduleId}/${encodeURIComponent(metaId)}`;
  const language = params.get("lang") ?? "";
  const query = new URLSearchParams();
  if (manual) query.set("title", title);
  if (language) query.set("lang", language);
  const search = query.size ? `?${query.toString()}` : "";
  const storageKey = `mangarr.add:${moduleId}:${manual ? title : metaId}`;
  // adding for a request (from the Requests page): link it when added
  const fromRequest = Number(params.get("request") ?? 0);
  if (fromRequest > 0) sessionState.set(storageKey + ":request", fromRequest);
  const requestId = fromRequest || sessionState.get<number>(storageKey + ":request", 0);
  const titles = [title, ...(meta.data?.altTitles ?? [])].filter(Boolean);
  return { manual, meta: meta.data ?? null, metaLoading: meta.isLoading, metaError: meta.error, title, titles, language, base, search, storageKey, requestId };
}

function usePicked(storageKey: string): [Picked[], (fn: (cur: Picked[]) => Picked[]) => void] {
  const [picked, setPicked] = useState<Picked[]>(() => sessionState.get<Picked[]>(storageKey + ":picked", []));
  useEffect(() => setPicked(sessionState.get<Picked[]>(storageKey + ":picked", [])), [storageKey]);
  const update = (fn: (cur: Picked[]) => Picked[]) =>
    setPicked((cur) => {
      const next = fn(cur);
      sessionState.set(storageKey + ":picked", next);
      return next;
    });
  return [picked, update];
}

/** Step 2 (/add/:moduleId/:metaId/sources): find the series at sources. */
export function AddSourcesStep() {
  const nav = useNavigate();
  const loc = useLocation();
  const ctx = useAddContext();
  const { data: roots } = useRootFolders();
  const [picked, setPicked] = usePicked(ctx.storageKey);
  const [sq, setSq] = useQueryParam("sq", ctx.title);
  const [scope, setScope] = useQueryParam("scope", "active");
  const [lang, setLang] = useQueryParam("lang");
  const [src, setSrc] = useQueryParam("src");
  const [more, setMore] = useQueryParam("more");
  const keys = src ? src.split(",") : [];
  const options = `${ctx.base}/options${ctx.search}`;
  const storedOptions = sessionState.get<Partial<Options>>(ctx.storageKey + ":options", {});
  const [rootId, setRootId] = useState(storedOptions.rootId ?? 0);
  const effectiveRootId = rootId || roots?.[0]?.id || 0;
  const selectRoot = (id: number) => {
    setRootId(id);
    sessionState.set(ctx.storageKey + ":options", { ...sessionState.get<Partial<Options>>(ctx.storageKey + ":options", {}), rootId: id });
  };

  if (ctx.metaLoading) return <Loading />;
  if (ctx.metaError) return <ErrorBox error={ctx.metaError} />;
  if (!ctx.title) return <ErrorBox error="Missing series title" />;
  const toggle = (p: Picked) => setPicked((cur) => (cur.some((x) => pickKey(x) === pickKey(p)) ? cur.filter((x) => pickKey(x) !== pickKey(p)) : [...cur, p]));
  return (
    <>
      <StepHeader step={1} title={ctx.title} base={ctx.base} search={ctx.search} />
      <Card
        title={
          <span>{t("Sources for") + " "}<span className="text-accent-2">{ctx.title}</span>
          </span>
        }
        actions={
          <>
            <Button size="sm" onClick={() => nav(`/add?q=${encodeURIComponent(ctx.title)}`)}>{t("Back")}</Button>
            <Button size="sm" variant="primary" disabled={picked.length === 0} onClick={() => nav(options, { state: { from: loc.pathname + loc.search } })}>{t("Next (")}{picked.length})
            </Button>
          </>
        }
      >
        <div className="mb-3 flex flex-wrap items-center gap-3">
          <p className="text-sm text-muted">{t("Pick one or more sources. Search order follows the selected library and language priorities.")}</p>
          {(roots?.length ?? 0) > 1 && (
            <Select className="ml-auto max-w-sm" value={effectiveRootId} onChange={(event) => selectRoot(Number(event.target.value))}>
              {roots?.map((root) => <option key={root.id} value={root.id}>{root.path}</option>)}
            </Select>
          )}
        </div>
        {picked.length > 0 && (
          <div className="mb-4 flex flex-col gap-1.5">
            {picked.map((p, i) => (
              <div key={pickKey(p)} className="flex items-center gap-2 rounded-md bg-panel-2 px-3 py-1.5 text-sm">
                <span className="w-5 text-muted">{i + 1}.</span>
                <Badge>{p.group.sourceName}</Badge>
                <span className="flex-1 truncate">{p.manga.title}</span>
                {p.manga.chapterCount != null && <span className="text-xs text-muted">{p.manga.chapterCount}{" " + t("ch")}</span>}
                <IconButton title={t("Up")} disabled={i === 0} onClick={() => setPicked((c) => swap(c, i, i - 1))}>
                  <ArrowUp className="size-3.5" />
                </IconButton>
                <IconButton title={t("Down")} disabled={i === picked.length - 1} onClick={() => setPicked((c) => swap(c, i, i + 1))}>
                  <ArrowDown className="size-3.5" />
                </IconButton>
                <IconButton title={t("Remove")} onClick={() => toggle(p)}>
                  <X className="size-3.5" />
                </IconButton>
              </div>
            ))}
          </div>
        )}
        <SourceSearch
          query={sq}
          setQuery={(v) => setSq(v, { replace: false })}
          titles={ctx.titles}
          scope={scope as Scope}
          setScope={(s) => setScope(s)}
          lang={lang}
          setLang={setLang}
          keys={keys}
          setKeys={(k) => setSrc(k.join(","))}
          more={more === "1"}
          setMore={(v) => setMore(v ? "1" : "", { replace: false })}
          selected={picked}
          rootFolderId={effectiveRootId}
          onPick={(m, g, use) => {
            const p = { manga: m, group: g };
            if (use) {
              // "Use this": keep it first and continue to the options
              setPicked((cur) => [p, ...cur.filter((x) => pickKey(x) !== pickKey(p))]);
              nav(options, { state: { from: loc.pathname + loc.search } });
            } else {
              toggle(p);
            }
          }}
        />
      </Card>
    </>
  );
}

type Options = {
  rootId: number;
  profileId: number;
  monitor: string;
  latestCount: number;
  fromChapter: number;
  monitorNew: string;
  searchMissing: boolean;
  direction: string;
};

/** Step 3 (/add/:moduleId/:metaId/options): monitoring options, then add. */
export function AddOptionsStep() {
  const nav = useNavigate();
  const loc = useLocation();
  const qc = useQueryClient();
  const toast = useToast();
  const ctx = useAddContext();
  const [picked] = usePicked(ctx.storageKey);
  const { data: roots } = useRootFolders();
  const { data: profiles } = useProfiles();
  const sourceSettings = useSettingsDoc<S["Sources"]>("sources");
  const languageDefaults = sourceSettings.value?.languageDefaults?.find((p) => p.language.toLowerCase() === ctx.language.toLowerCase());
  const defaults: Options = {
    rootId: 0,
    profileId: 0,
    monitor: "all",
    latestCount: 5,
    fromChapter: 1,
    monitorNew: "all",
    searchMissing: true,
    direction: "__default",
  };
  const [o, setO] = useState<Options>(() => ({ ...defaults, ...sessionState.get<Partial<Options>>(ctx.storageKey + ":options", {}) }));
  const patch = (p: Partial<Options>) =>
    setO((cur) => {
      const next = { ...cur, ...p };
      sessionState.set(ctx.storageKey + ":options", next);
      return next;
    });
  const [adding, setAdding] = useState(false);
  const back = (loc.state as { from?: string } | null)?.from ?? `${ctx.base}/sources${ctx.search}`;

  if (ctx.metaLoading) return <Loading />;
  const rootId = o.rootId || languageDefaults?.rootFolderId || roots?.find((r) => r.language === ctx.language)?.id || roots?.[0]?.id || 0;
  const profileId = o.profileId || languageDefaults?.profileId || profiles?.find((p) => p.isDefault)?.id || profiles?.[0]?.id || 0;
  const direction = o.direction === "__default" ? (languageDefaults?.readingDirection || (ctx.meta?.format === "manhwa" || ctx.meta?.format === "manhua" ? "webtoon" : "")) : o.direction;

  const add = async () => {
    setAdding(true);
    try {
      const s = await unwrap(
        api.POST("/api/v1/series", {
          body: {
            metadata: ctx.meta ? { moduleId: ctx.meta.moduleId, provider: ctx.meta.provider, id: ctx.meta.id } : undefined,
            title: ctx.meta ? undefined : ctx.title,
            sources: picked.map((p) => ({
              moduleId: p.group.moduleId,
              sourceId: p.group.sourceId,
              url: p.manga.url,
              engineRef: p.manga.engineRef,
              title: p.manga.title,
              sourceName: p.group.sourceName,
              lang: p.group.lang,
            })),
            language: ctx.language || picked[0]?.group.lang || undefined,
            rootFolderId: rootId,
            profileId: profileId || undefined,
            monitor: o.monitor as AddRequest["monitor"],
            latestCount: o.monitor === "latest" ? o.latestCount : undefined,
            fromChapter: o.monitor === "from" ? o.fromChapter : undefined,
            monitorNew: o.monitorNew as AddRequest["monitorNew"],
            searchMissing: o.searchMissing,
            readingDirection: (direction || undefined) as AddRequest["readingDirection"],
            requestId: ctx.requestId || undefined,
          },
        }),
      );
      sessionState.remove(ctx.storageKey + ":picked");
      sessionState.remove(ctx.storageKey + ":options");
      sessionState.remove(ctx.storageKey + ":request");
      qc.invalidateQueries({ queryKey: ["series"] });
      qc.invalidateQueries({ queryKey: ["requests"] });
      toast.success(`${s.title} added`, tr("Fetching chapters…"));
      nav(`/series/${s.id}`);
    } catch (e) {
      toast.fromError(e, tr("Could not add series"));
    } finally {
      setAdding(false);
    }
  };

  return (
    <>
      <StepHeader step={2} title={ctx.title} base={ctx.base} search={ctx.search} />
      <Card
        title={t("Options")}
        actions={
          <>
            <Button size="sm" onClick={() => nav(back)}>{t("Back")}</Button>
            <Button size="sm" variant="primary" loading={adding} disabled={!picked.length} icon={<BookPlus className="size-4" />} onClick={add}>{t("Add") + " "}{ctx.title}
            </Button>
          </>
        }
      >
        {!picked.length && <ErrorBox error="Pick at least one source first." />}
        {picked.length > 0 && (
          <p className="mb-4 text-sm text-muted">{t("Sources:")}{" "}
            {picked.map((p, i) => (
              <span key={pickKey(p)}>
                {i > 0 && " → "}
                <b className="text-fg">{p.group.sourceName}</b>
              </span>
            ))}
          </p>
        )}
        {!roots?.length ? (
          <ErrorBox error="Add a root folder in Settings → Media management first." />
        ) : (
          <div className="grid gap-4 md:grid-cols-2">
            <Field label={t("Root folder")}>
              <Select value={rootId} onChange={(e) => patch({ rootId: Number(e.target.value) })}>
                {roots.map((r) => (
                  <option key={r.id} value={r.id}>
                    {r.path} {r.language ? `(${r.language})` : ""}
                  </option>
                ))}
              </Select>
            </Field>
            <Field label={t("Profile")}>
              <Select value={profileId} onChange={(e) => patch({ profileId: Number(e.target.value) })}>
                {profiles?.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                    {p.isDefault ? tr(" (default)") : ""}
                  </option>
                ))}
              </Select>
            </Field>
            <Field label={t("Monitor")} help={t("Which existing chapters to download. Future chapters follow the setting below.")}>
              <Select value={o.monitor} onChange={(e) => patch({ monitor: e.target.value })}>
                <option value="all">{t("All chapters")}</option>
                <option value="latest">{t("Latest N chapters")}</option>
                <option value="from">{t("From chapter…")}</option>
                <option value="future">{t("Only future chapters")}</option>
                <option value="none">{t("None")}</option>
              </Select>
            </Field>
            {o.monitor === "latest" && (
              <Field label={t("Number of latest chapters")}>
                <Input type="number" min={1} value={o.latestCount} onChange={(e) => patch({ latestCount: Number(e.target.value) })} />
              </Field>
            )}
            {o.monitor === "from" && (
              <Field label={t("First chapter")}>
                <Input type="number" step="0.1" value={o.fromChapter} onChange={(e) => patch({ fromChapter: Number(e.target.value) })} />
              </Field>
            )}
            <Field label={t("New chapters")}>
              <Select value={o.monitorNew} onChange={(e) => patch({ monitorNew: e.target.value })}>
                <option value="all">{t("Monitor and download")}</option>
                <option value="none">{t("Don't monitor")}</option>
              </Select>
            </Field>
            <Field label={t("Reading direction")}>
              <Select value={direction} onChange={(e) => patch({ direction: e.target.value })}>
                <option value="">{t("Automatic")}</option>
                <option value="rtl">{t("Right to left (manga)")}</option>
                <option value="ltr">{t("Left to right")}</option>
                <option value="webtoon">{t("Webtoon (long strip)")}</option>
              </Select>
            </Field>
            <div className="md:col-span-2">
              <Switch checked={o.searchMissing} onChange={(v) => patch({ searchMissing: v })} label={t("Start downloading monitored chapters right away")} />
            </div>
          </div>
        )}
      </Card>
    </>
  );
}

function swap<T>(a: T[], i: number, j: number): T[] {
  const c = [...a];
  [c[i], c[j]] = [c[j], c[i]];
  return c;
}
