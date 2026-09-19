import { useUIMode } from "../../lib/uiPreferences";
import { t } from "../../lib/i18n/core";
import { useEffect, useState } from "react";
import { Link } from "react-router";
import { CheckSquare, LayoutGrid, List, PlusCircle, RefreshCw, Search } from "lucide-react";
import { apiUrl, type Series } from "../../api/client";
import { usePushCommand, useRootFolders, useSeriesSearch } from "../../api/queries";
import { Cover } from "../../components/Cover";
import { Badge, Button, EmptyState, ErrorBox, Input, Loading, PageHeader, Progress, Select, Table, Td, Th } from "../../components/ui";
import { bytes, date } from "../../lib/format";
import { useListParam, useQueryParam } from "../../lib/urlState";
import { MassEditBar } from "./Organize";
import { ContinueReading } from "./ContinueReading";
import { useAccount } from "../../lib/account";

type Filter = "all" | "monitored" | "missing" | "ongoing" | "completed" | "unread" | "reading" | "following";
type Sort = "title" | "added" | "latest" | "missing" | "size" | "read";

export function statusTone(s: string) {
  return s === "ongoing" ? "ok" : s === "completed" ? "info" : s === "hiatus" ? "warn" : s === "cancelled" ? "err" : "default";
}

function progressOf(s: Series) {
  const { fileCount, monitoredCount, missingCount } = s.stats;
  if (monitoredCount === 0) return { pct: 0, tone: "accent" as const };
  const pct = ((monitoredCount - missingCount) / monitoredCount) * 100;
  return { pct, tone: missingCount === 0 ? ("ok" as const) : fileCount === 0 ? ("err" as const) : ("warn" as const) };
}

/** ReadBar is a thin bar of chapters read. */
function ReadBar({ s }: { s: Series }) {
  const pct = s.stats.chapterCount > 0 ? Math.min(100, (s.stats.readCount / s.stats.chapterCount) * 100) : 0;
  return (
    <div className="-mt-1 h-1 overflow-hidden rounded-full bg-panel-2" title={`${s.stats.readCount} of ${s.stats.chapterCount} read`}>
      <div className="h-full rounded-full bg-info" style={{ width: `${pct}%` }} />
    </div>
  );
}

export function SeriesIndex() {
  const { data: roots } = useRootFolders();
  const { editing } = useUIMode();
  const manage = useAccount().can("library.manage") && editing;
  const push = usePushCommand();
  const [q, setQ] = useQueryParam("q");
  const [filterParam, setFilter] = useListParam("filter", "all");
  const [sortParam, setSort] = useListParam("sort", "title");
  const [rootParam, setRoot] = useListParam("root", "");
  const [language, setLanguage] = useListParam("language", "");
  const [pageParam, setPage] = useListParam("page", "1");
  const [pageSizeParam, setPageSize] = useListParam("pageSize", "36");
  const filter = filterParam as Filter;
  const sort = sortParam as Sort;
  const page = Math.max(1, Number(pageParam) || 1);
  const pageSize = [24, 36, 48, 72].includes(Number(pageSizeParam)) ? Number(pageSizeParam) : 36;
  const rootFolderId = Number(rootParam) || undefined;
  const [searchDraft, setSearchDraft] = useState(q);
  useEffect(() => setSearchDraft(q), [q]);
  useEffect(() => {
    if (searchDraft === q) return;
    const timeout = window.setTimeout(() => {
      setQ(searchDraft);
      setPage("1");
    }, 250);
    return () => window.clearTimeout(timeout);
  }, [searchDraft, q]);
  const { data, isLoading, isFetching, error } = useSeriesSearch({ q: q || undefined, filter, sort, rootFolderId, language: language || undefined, page, pageSize });
  useEffect(() => {
    if (!data?.total) return;
    const last = Math.max(1, Math.ceil(data.total / pageSize));
    if (page > last) setPage(String(last));
  }, [data?.total, page, pageSize, setPage]);
  const [selecting, setSelecting] = useState(false);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  useEffect(()=>{if(!manage){setSelecting(false);setSelected(new Set());}},[manage]);
  const toggle = (id: number) =>
    setSelected((cur) => {
      const n = new Set(cur);
      if (n.has(id)) n.delete(id);
      else n.add(id);
      return n;
    });
  const [view, setView] = useState<"posters" | "table">(() => (localStorage.getItem("seriesView") as "posters" | "table") || "posters");

  const list = data?.items ?? [];
  const setFilterAndReset = (value: string) => { setFilter(value); setPage("1"); };
  const setSortAndReset = (value: string) => { setSort(value); setPage("1"); };

  const setViewPersist = (v: "posters" | "table") => {
    setView(v);
    localStorage.setItem("seriesView", v);
  };

  return (
    <>
      <PageHeader
        title={t("Series")}
        subtitle={data ? `${data.total} series · ${bytes(data.totalSize)}` : undefined}
        actions={
          manage && (
            <>
              <Button icon={<RefreshCw className="size-4" />} onClick={() => push.mutate({ name: "RefreshSources", label: "Checking sources for new chapters" })}>{t("Check now")}</Button>
              <Link to="/add">
                <Button variant="primary" icon={<PlusCircle className="size-4" />}>{t("Add series")}</Button>
              </Link>
            </>
          )
        }
      />
      {!q && filter === "all" && !rootFolderId && !language && <ContinueReading />}
      <div className="mb-4 flex flex-wrap items-center gap-2">
        <div className="relative w-full max-w-xs">
          <Search className="absolute left-2.5 top-2.5 size-4 text-muted" />
          <Input className="pl-8" placeholder={t("Search titles and alternative titles…")} value={searchDraft} onChange={(e) => setSearchDraft(e.target.value)} />
        </div>
        <Select className="w-auto" value={filter} onChange={(e) => setFilterAndReset(e.target.value)}>
          <option value="all">{t("All")}</option>
          <option value="following">{t("Following")}</option>
          <option value="monitored">{t("Monitored")}</option>
          <option value="missing">{t("Missing chapters")}</option>
          <option value="ongoing">{t("Ongoing")}</option>
          <option value="completed">{t("Completed")}</option>
          <option value="unread">{t("With unread chapters")}</option>
          <option value="reading">{t("Started reading")}</option>
        </Select>
        <Select className="w-auto" value={sort} onChange={(e) => setSortAndReset(e.target.value)}>
          <option value="title">{t("Sort: title")}</option>
          <option value="added">{t("Sort: recently added")}</option>
          <option value="latest">{t("Sort: latest chapter")}</option>
          <option value="missing">{t("Sort: missing")}</option>
          <option value="size">{t("Sort: size")}</option>
          <option value="read">{t("Sort: recently read")}</option>
        </Select>
        {(roots?.length ?? 0) > 1 && (
          <Select className="w-auto max-w-xs" value={rootParam} onChange={(e) => { setRoot(e.target.value); setPage("1"); }}>
            <option value="">{t("All libraries")}</option>
            {roots?.map((root) => <option key={root.id} value={root.id}>{root.path}</option>)}
          </Select>
        )}
        {(data?.languages?.length ?? 0) > 1 && (
          <Select className="w-auto" value={language} onChange={(e) => { setLanguage(e.target.value); setPage("1"); }}>
            <option value="">{t("All languages")}</option>
            {data?.languages?.map((item) => <option key={item} value={item}>{item}</option>)}
          </Select>
        )}
        <div className="ml-auto flex gap-1">
          {manage && (
            <Button
              size="sm"
              variant={selecting ? "primary" : "secondary"}
              icon={<CheckSquare className="size-3.5" />}
              onClick={() => (setSelecting(!selecting), setSelected(new Set()))}
            >{t("Select")}</Button>
          )}
          {selecting && (
            <Button size="sm" onClick={() => setSelected(new Set(list.map((s) => s.id)))}>{t("All shown")}</Button>
          )}
          <Button variant={view === "posters" ? "primary" : "secondary"} size="sm" onClick={() => setViewPersist("posters")} icon={<LayoutGrid className="size-3.5" />} />
          <Button variant={view === "table" ? "primary" : "secondary"} size="sm" onClick={() => setViewPersist("table")} icon={<List className="size-3.5" />} />
        </div>
      </div>
      {isFetching && data && <div className="mb-2 h-0.5 overflow-hidden rounded bg-panel-2"><div className="h-full w-1/3 animate-pulse rounded bg-accent" /></div>}
      {isLoading && <Loading />}
      {error && <ErrorBox error={error} />}
      {data && data.total === 0 && !q && filter === "all" && !rootFolderId && !language && (
        <EmptyState title={t("No series yet")}>{manage?t("Add a source module (Settings → Source modules), a root folder (Settings → Media management), then add your first series."):t("No series available yet.")}</EmptyState>
      )}
      {data && data.total === 0 && (q || filter !== "all" || rootFolderId || language) && (
        <EmptyState title={t("No series match these filters")}>{t("Try a shorter title or clear one of the filters.")}</EmptyState>
      )}
      {view === "posters" ? (
        <div className="grid grid-cols-[repeat(auto-fill,minmax(150px,1fr))] gap-4">
          {list.map((s) => {
            const p = progressOf(s);
            return (
              <Link
                key={s.id}
                to={`/series/${s.id}`}
                onClick={(e) => {
                  if (selecting) {
                    e.preventDefault();
                    toggle(s.id);
                  }
                }}
                className={`group flex flex-col gap-2 ${selecting && selected.has(s.id) ? "rounded-md ring-2 ring-accent ring-offset-2 ring-offset-bg" : ""}`}
              >
                <div className="relative">
                  <Cover src={apiUrl(s.coverUrl)} alt={s.title} className="aspect-[2/3] w-full ring-accent/60 transition group-hover:ring-2" />
                  {manage && !s.monitored && <div className="absolute left-1.5 top-1.5"><Badge>{t("unmonitored")}</Badge></div>}
                  {manage && s.stats.missingCount > 0 && (
                    <div className="absolute right-1.5 top-1.5">
                      <Badge tone="warn">{s.stats.missingCount}{" " + t("missing")}</Badge>
                    </div>
                  )}
                </div>
                <Progress value={p.pct} tone={p.tone} />
                {s.stats.readCount > 0 && <ReadBar s={s} />}
                <div className="line-clamp-2 text-sm font-medium leading-tight">{s.title}</div>
                {(s.editions?.length ?? 0) > 0 && (
                  <div className="flex flex-wrap gap-1">
                    {(s.editions ?? []).map((edition) => <Badge key={edition.id}>{edition.language || "—"}</Badge>)}
                  </div>
                )}
                <div className="-mt-1 text-xs text-muted">
                  {s.stats.fileCount}/{s.stats.chapterCount}{" " + t("chapters")}{s.stats.readCount > 0 && ` · ${s.stats.readCount} read`}
                </div>
              </Link>
            );
          })}
        </div>
      ) : (
        <Table>
          <thead>
            <tr>
              {selecting && <Th className="w-8" />}
              <Th>{t("Title")}</Th>
              <Th>{t("Status")}</Th>
              <Th>{t("Chapters")}</Th>
              <Th>{t("Latest")}</Th>
              <Th>{t("Size")}</Th>
              <Th>{t("Added")}</Th>
            </tr>
          </thead>
          <tbody>
            {list.map((s) => (
              <tr key={s.id} className="hover:bg-panel-2/60">
                {selecting && (
                  <Td className="w-8">
                    <input type="checkbox" aria-label={`Select ${s.title}`} checked={selected.has(s.id)} onChange={() => toggle(s.id)} />
                  </Td>
                )}
                <Td>
                  <Link to={`/series/${s.id}`} className="font-medium hover:text-accent-2">
                    {s.title}
                  </Link>
                  {(s.editions?.length ?? 0) > 0 && (
                    <span className="ml-2 inline-flex gap-1">
                      {(s.editions ?? []).map((edition) => <Badge key={edition.id}>{edition.language || "—"}</Badge>)}
                    </span>
                  )}
                  {manage && !s.monitored && <span className="ml-2"><Badge>{t("unmonitored")}</Badge></span>}
                </Td>
                <Td>
                  <Badge tone={statusTone(s.status)}>{s.status}</Badge>
                </Td>
                <Td className="w-48">
                  <div className="flex items-center gap-2">
                    <div className="w-24"><Progress value={progressOf(s).pct} tone={progressOf(s).tone} /></div>
                    <span className="text-xs text-muted">
                      {s.stats.fileCount}/{s.stats.chapterCount}
                      {s.stats.readCount > 0 && ` · ${s.stats.readCount} read`}
                    </span>
                  </div>
                </Td>
                <Td>{s.stats.lastChapter || "—"}</Td>
                <Td>{bytes(s.stats.sizeOnDisk)}</Td>
                <Td>{date(s.addedAt)}</Td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      {data && data.total > 0 && (
        <div className="mt-5 flex flex-wrap items-center justify-center gap-2">
          <Button size="sm" disabled={page <= 1} onClick={() => setPage(String(page - 1))}>{t("Previous")}</Button>
          <span className="px-2 text-sm text-muted">{t("Page")} {page} {t("of")} {Math.max(1, Math.ceil(data.total / pageSize))}</span>
          <Button size="sm" disabled={page * pageSize >= data.total} onClick={() => setPage(String(page + 1))}>{t("Next")}</Button>
          <Select className="ml-2 w-auto" value={pageSize} onChange={(e) => { setPageSize(e.target.value); setPage("1"); }}>
            {[24, 36, 48, 72].map((size) => <option key={size} value={size}>{size} {t("per page")}</option>)}
          </Select>
        </div>
      )}
      {selecting && selected.size > 0 && (
        <>
          <div className="h-20" />
          <MassEditBar ids={[...selected]} onClear={() => setSelected(new Set())} />
        </>
      )}
    </>
  );
}
