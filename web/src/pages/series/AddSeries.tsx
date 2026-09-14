import { useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowDown, ArrowUp, BookPlus, Search, X } from "lucide-react";
import { api, unwrap, type AddRequest, type LookupResult } from "../../api/client";
import { useProfiles, useRootFolders } from "../../api/queries";
import { Cover } from "../../components/Cover";
import { Badge, Button, Card, ErrorBox, Field, IconButton, Input, Loading, PageHeader, Select, Switch } from "../../components/ui";
import { useToast } from "../../lib/toast";
import { SourceResults, SourceSearchBar, type Picked } from "./SourceSearch";

/** MetadataSearch looks up series across metadata modules. */
export function MetadataSearch({ initialQuery = "", onPick }: { initialQuery?: string; onPick: (c: LookupResult) => void }) {
  const [draft, setDraft] = useState(initialQuery);
  const [query, setQuery] = useState(initialQuery);
  const { data, isFetching, error } = useQuery({
    queryKey: ["lookup", query],
    queryFn: () => unwrap(api.GET("/api/v1/series/lookup", { params: { query: { q: query } } })),
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
        <Input autoFocus value={draft} onChange={(e) => setDraft(e.target.value)} placeholder="Search by title (AniList and other metadata modules)" />
        <Button type="submit" variant="primary" icon={<Search className="size-4" />}>
          Search
        </Button>
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
              {r.existingSeriesId ? (
                <Link to={`/series/${r.existingSeriesId}`}>
                  <Button size="sm">In library</Button>
                </Link>
              ) : (
                <Button size="sm" variant="primary" onClick={() => onPick(r)}>
                  Select
                </Button>
              )}
            </div>
          </div>
        ))}
        {data && data.results.length === 0 && <p className="text-sm text-muted">No metadata found.</p>}
      </div>
    </div>
  );
}

export function AddSeries() {
  const nav = useNavigate();
  const qc = useQueryClient();
  const toast = useToast();
  const { data: roots } = useRootFolders();
  const { data: profiles } = useProfiles();
  const [params] = useSearchParams();
  const prefill = params.get("q") ?? "";
  const [step, setStep] = useState<1 | 2 | 3>(1);
  const [meta, setMeta] = useState<LookupResult | null>(null);
  const [title, setTitle] = useState(prefill);
  const [query, setQuery] = useState("");
  const [lang, setLang] = useState("en");
  const [picked, setPicked] = useState<Picked[]>([]);
  const [rootId, setRootId] = useState<number>(0);
  const [profileId, setProfileId] = useState<number>(0);
  const [monitor, setMonitor] = useState("all");
  const [latestCount, setLatestCount] = useState(5);
  const [fromChapter, setFromChapter] = useState(1);
  const [monitorNew, setMonitorNew] = useState("all");
  const [searchMissing, setSearchMissing] = useState(true);
  const [direction, setDirection] = useState("");
  const [adding, setAdding] = useState(false);

  const pickMeta = (c: LookupResult | null, t: string) => {
    setMeta(c);
    setTitle(t);
    setQuery(t);
    if (c?.format === "manhwa" || c?.format === "manhua") setDirection("webtoon");
    setStep(2);
  };

  const togglePick = (p: Picked) => {
    const key = (x: Picked) => `${x.group.moduleId}:${x.group.sourceId}:${x.manga.url}`;
    setPicked((cur) => (cur.some((x) => key(x) === key(p)) ? cur.filter((x) => key(x) !== key(p)) : [...cur, p]));
  };

  const add = async () => {
    setAdding(true);
    try {
      const s = await unwrap(
        api.POST("/api/v1/series", {
          body: {
            metadata: meta ? { moduleId: meta.moduleId, provider: meta.provider, id: meta.id } : undefined,
            title: meta ? undefined : title,
            sources: picked.map((p) => ({
              moduleId: p.group.moduleId,
              sourceId: p.group.sourceId,
              url: p.manga.url,
              engineRef: p.manga.engineRef,
              title: p.manga.title,
              sourceName: p.group.sourceName,
              lang: p.group.lang,
            })),
            rootFolderId: rootId || roots?.[0]?.id || 0,
            profileId: profileId || undefined,
            monitor: monitor as AddRequest["monitor"],
            latestCount: monitor === "latest" ? latestCount : undefined,
            fromChapter: monitor === "from" ? fromChapter : undefined,
            monitorNew: monitorNew as AddRequest["monitorNew"],
            searchMissing,
            readingDirection: (direction || undefined) as AddRequest["readingDirection"],
          },
        }),
      );
      qc.invalidateQueries({ queryKey: ["series"] });
      toast.success(`${s.title} added`, "Fetching chapters…");
      nav(`/series/${s.id}`);
    } catch (e) {
      toast.fromError(e, "Could not add series");
    } finally {
      setAdding(false);
    }
  };

  return (
    <>
      <PageHeader title="Add series" subtitle={`Step ${step} of 3 · ${["Metadata", "Sources", "Options"][step - 1]}`} />
      {step === 1 && (
        <Card>
          <MetadataSearch initialQuery={prefill} onPick={(c) => pickMeta(c, c.title)} />
          <div className="mt-4 border-t border-border pt-4">
            <p className="mb-2 text-sm text-muted">Not on any metadata site? Add it using only the source's information.</p>
            <form
              className="flex gap-2"
              onSubmit={(e) => {
                e.preventDefault();
                if (title.trim()) pickMeta(null, title.trim());
              }}
            >
              <Input value={title} onChange={(e) => setTitle(e.target.value)} placeholder="Series title" />
              <Button type="submit">Continue without metadata</Button>
            </form>
          </div>
        </Card>
      )}

      {step === 2 && (
        <Card
          title={
            <span>
              Sources for <span className="text-accent-2">{title}</span>
            </span>
          }
          actions={
            <>
              <Button size="sm" onClick={() => setStep(1)}>
                Back
              </Button>
              <Button size="sm" variant="primary" disabled={picked.length === 0} onClick={() => setStep(3)}>
                Next ({picked.length})
              </Button>
            </>
          }
        >
          <p className="mb-3 text-sm text-muted">Pick one or more sources. The first one has the highest priority; others are fallbacks.</p>
          {picked.length > 0 && (
            <div className="mb-4 flex flex-col gap-1.5">
              {picked.map((p, i) => (
                <div key={i} className="flex items-center gap-2 rounded-md bg-panel-2 px-3 py-1.5 text-sm">
                  <span className="w-5 text-muted">{i + 1}.</span>
                  <Badge>{p.group.sourceName}</Badge>
                  <span className="flex-1 truncate">{p.manga.title}</span>
                  <IconButton title="Up" disabled={i === 0} onClick={() => setPicked((c) => swap(c, i, i - 1))}>
                    <ArrowUp className="size-3.5" />
                  </IconButton>
                  <IconButton title="Down" disabled={i === picked.length - 1} onClick={() => setPicked((c) => swap(c, i, i + 1))}>
                    <ArrowDown className="size-3.5" />
                  </IconButton>
                  <IconButton title="Remove" onClick={() => togglePick(p)}>
                    <X className="size-3.5" />
                  </IconButton>
                </div>
              ))}
            </div>
          )}
          <SourceSearchBar query={query} setQuery={setQuery} lang={lang} setLang={setLang} />
          <SourceResults query={query} lang={lang} selected={picked} onPick={(m, g) => togglePick({ manga: m, group: g })} />
        </Card>
      )}

      {step === 3 && (
        <Card
          title="Options"
          actions={
            <>
              <Button size="sm" onClick={() => setStep(2)}>
                Back
              </Button>
              <Button size="sm" variant="primary" loading={adding} icon={<BookPlus className="size-4" />} onClick={add}>
                Add {title}
              </Button>
            </>
          }
        >
          {!roots?.length ? (
            <ErrorBox error="Add a root folder in Settings → Media management first." />
          ) : (
            <div className="grid gap-4 md:grid-cols-2">
              <Field label="Root folder">
                <Select value={rootId || roots[0].id} onChange={(e) => setRootId(Number(e.target.value))}>
                  {roots.map((r) => (
                    <option key={r.id} value={r.id}>
                      {r.path} {r.language ? `(${r.language})` : ""}
                    </option>
                  ))}
                </Select>
              </Field>
              <Field label="Profile">
                <Select
                  value={profileId || profiles?.find((p) => p.isDefault)?.id || profiles?.[0]?.id || 0}
                  onChange={(e) => setProfileId(Number(e.target.value))}
                >
                  {profiles?.map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.name}
                      {p.isDefault ? " (default)" : ""}
                    </option>
                  ))}
                </Select>
              </Field>
              <Field label="Monitor" help="Which existing chapters to download. Future chapters follow the setting below.">
                <Select value={monitor} onChange={(e) => setMonitor(e.target.value)}>
                  <option value="all">All chapters</option>
                  <option value="latest">Latest N chapters</option>
                  <option value="from">From chapter…</option>
                  <option value="future">Only future chapters</option>
                  <option value="none">None</option>
                </Select>
              </Field>
              {monitor === "latest" && (
                <Field label="Number of latest chapters">
                  <Input type="number" min={1} value={latestCount} onChange={(e) => setLatestCount(Number(e.target.value))} />
                </Field>
              )}
              {monitor === "from" && (
                <Field label="First chapter">
                  <Input type="number" step="0.1" value={fromChapter} onChange={(e) => setFromChapter(Number(e.target.value))} />
                </Field>
              )}
              <Field label="New chapters">
                <Select value={monitorNew} onChange={(e) => setMonitorNew(e.target.value)}>
                  <option value="all">Monitor and download</option>
                  <option value="none">Don't monitor</option>
                </Select>
              </Field>
              <Field label="Reading direction">
                <Select value={direction} onChange={(e) => setDirection(e.target.value)}>
                  <option value="">Automatic</option>
                  <option value="rtl">Right to left (manga)</option>
                  <option value="ltr">Left to right</option>
                  <option value="webtoon">Webtoon (long strip)</option>
                </Select>
              </Field>
              <div className="md:col-span-2">
                <Switch checked={searchMissing} onChange={setSearchMissing} label="Start downloading monitored chapters right away" />
              </div>
            </div>
          )}
        </Card>
      )}
    </>
  );
}

function swap<T>(a: T[], i: number, j: number): T[] {
  const c = [...a];
  [c[i], c[j]] = [c[j], c[i]];
  return c;
}
