import { useMemo, useState } from "react";
import { Link } from "react-router";
import { CheckSquare, LayoutGrid, List, PlusCircle, RefreshCw, Search } from "lucide-react";
import { apiUrl, type Series } from "../../api/client";
import { usePushCommand, useSeriesList } from "../../api/queries";
import { Cover } from "../../components/Cover";
import { Badge, Button, EmptyState, ErrorBox, Input, Loading, PageHeader, Progress, Select, Table, Td, Th } from "../../components/ui";
import { bytes, date } from "../../lib/format";
import { useQueryParam } from "../../lib/urlState";
import { MassEditBar } from "./Organize";

type Filter = "all" | "monitored" | "missing" | "ongoing" | "completed";
type Sort = "title" | "added" | "latest" | "missing" | "size";

export function statusTone(s: string) {
  return s === "ongoing" ? "ok" : s === "completed" ? "info" : s === "hiatus" ? "warn" : s === "cancelled" ? "err" : "default";
}

function progressOf(s: Series) {
  const { fileCount, monitoredCount, missingCount } = s.stats;
  if (monitoredCount === 0) return { pct: 0, tone: "accent" as const };
  const pct = ((monitoredCount - missingCount) / monitoredCount) * 100;
  return { pct, tone: missingCount === 0 ? ("ok" as const) : fileCount === 0 ? ("err" as const) : ("warn" as const) };
}

export function SeriesIndex() {
  const { data, isLoading, error } = useSeriesList();
  const push = usePushCommand();
  const [q, setQ] = useQueryParam("q");
  const [filterParam, setFilter] = useQueryParam("filter", "all");
  const [sortParam, setSort] = useQueryParam("sort", "title");
  const filter = filterParam as Filter;
  const sort = sortParam as Sort;
  const [selecting, setSelecting] = useState(false);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const toggle = (id: number) =>
    setSelected((cur) => {
      const n = new Set(cur);
      if (n.has(id)) n.delete(id);
      else n.add(id);
      return n;
    });
  const [view, setView] = useState<"posters" | "table">(() => (localStorage.getItem("seriesView") as "posters" | "table") || "posters");

  const list = useMemo(() => {
    let l = [...(data ?? [])];
    const needle = q.trim().toLowerCase();
    if (needle) l = l.filter((s) => s.title.toLowerCase().includes(needle) || s.metadata.altTitles?.some((t) => t.toLowerCase().includes(needle)));
    if (filter === "monitored") l = l.filter((s) => s.monitored);
    if (filter === "missing") l = l.filter((s) => s.stats.missingCount > 0);
    if (filter === "ongoing") l = l.filter((s) => s.status === "ongoing");
    if (filter === "completed") l = l.filter((s) => s.status === "completed");
    const by: Record<Sort, (a: Series, b: Series) => number> = {
      title: (a, b) => a.sortTitle.localeCompare(b.sortTitle),
      added: (a, b) => b.addedAt.localeCompare(a.addedAt),
      latest: (a, b) => b.stats.lastChapter - a.stats.lastChapter,
      missing: (a, b) => b.stats.missingCount - a.stats.missingCount,
      size: (a, b) => b.stats.sizeOnDisk - a.stats.sizeOnDisk,
    };
    return l.sort(by[sort]);
  }, [data, q, filter, sort]);

  const setViewPersist = (v: "posters" | "table") => {
    setView(v);
    localStorage.setItem("seriesView", v);
  };

  return (
    <>
      <PageHeader
        title="Series"
        subtitle={data ? `${data.length} series · ${bytes(data.reduce((n, s) => n + s.stats.sizeOnDisk, 0))}` : undefined}
        actions={
          <>
            <Button icon={<RefreshCw className="size-4" />} onClick={() => push.mutate({ name: "RefreshSources", label: "Checking sources for new chapters" })}>
              Check now
            </Button>
            <Link to="/add">
              <Button variant="primary" icon={<PlusCircle className="size-4" />}>
                Add series
              </Button>
            </Link>
          </>
        }
      />
      <div className="mb-4 flex flex-wrap items-center gap-2">
        <div className="relative w-full max-w-xs">
          <Search className="absolute left-2.5 top-2.5 size-4 text-muted" />
          <Input className="pl-8" placeholder="Filter series…" defaultValue={q} onChange={(e) => setQ(e.target.value)} />
        </div>
        <Select className="w-auto" value={filter} onChange={(e) => setFilter(e.target.value as Filter)}>
          <option value="all">All</option>
          <option value="monitored">Monitored</option>
          <option value="missing">Missing chapters</option>
          <option value="ongoing">Ongoing</option>
          <option value="completed">Completed</option>
        </Select>
        <Select className="w-auto" value={sort} onChange={(e) => setSort(e.target.value as Sort)}>
          <option value="title">Sort: title</option>
          <option value="added">Sort: recently added</option>
          <option value="latest">Sort: latest chapter</option>
          <option value="missing">Sort: missing</option>
          <option value="size">Sort: size</option>
        </Select>
        <div className="ml-auto flex gap-1">
          <Button
            size="sm"
            variant={selecting ? "primary" : "secondary"}
            icon={<CheckSquare className="size-3.5" />}
            onClick={() => (setSelecting(!selecting), setSelected(new Set()))}
          >
            Select
          </Button>
          {selecting && (
            <Button size="sm" onClick={() => setSelected(new Set(list.map((s) => s.id)))}>
              All shown
            </Button>
          )}
          <Button variant={view === "posters" ? "primary" : "secondary"} size="sm" onClick={() => setViewPersist("posters")} icon={<LayoutGrid className="size-3.5" />} />
          <Button variant={view === "table" ? "primary" : "secondary"} size="sm" onClick={() => setViewPersist("table")} icon={<List className="size-3.5" />} />
        </div>
      </div>
      {isLoading && <Loading />}
      {error && <ErrorBox error={error} />}
      {data && data.length === 0 && (
        <EmptyState title="No series yet">
          Add a source module (Settings → Source modules), a root folder (Settings → Media management), then add your first series.
        </EmptyState>
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
                  {!s.monitored && <div className="absolute left-1.5 top-1.5"><Badge>unmonitored</Badge></div>}
                  {s.stats.missingCount > 0 && (
                    <div className="absolute right-1.5 top-1.5">
                      <Badge tone="warn">{s.stats.missingCount} missing</Badge>
                    </div>
                  )}
                </div>
                <Progress value={p.pct} tone={p.tone} />
                <div className="line-clamp-2 text-sm font-medium leading-tight">{s.title}</div>
                <div className="-mt-1 text-xs text-muted">
                  {s.stats.fileCount}/{s.stats.chapterCount} chapters
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
              <Th>Title</Th>
              <Th>Status</Th>
              <Th>Chapters</Th>
              <Th>Latest</Th>
              <Th>Size</Th>
              <Th>Added</Th>
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
                  {!s.monitored && <span className="ml-2"><Badge>unmonitored</Badge></span>}
                </Td>
                <Td>
                  <Badge tone={statusTone(s.status)}>{s.status}</Badge>
                </Td>
                <Td className="w-48">
                  <div className="flex items-center gap-2">
                    <div className="w-24"><Progress value={progressOf(s).pct} tone={progressOf(s).tone} /></div>
                    <span className="text-xs text-muted">
                      {s.stats.fileCount}/{s.stats.chapterCount}
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
      {selecting && selected.size > 0 && (
        <>
          <div className="h-20" />
          <MassEditBar ids={[...selected]} onClear={() => setSelected(new Set())} />
        </>
      )}
    </>
  );
}
