import { t as tr, t } from "../../lib/i18n/core";
import { Fragment, memo, useCallback, useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowDownToLine, ArrowUpToLine, BookOpen, ChevronDown, ChevronRight, ExternalLink, HelpCircle, Pause, Play, RotateCcw, RotateCw, Search, Sparkles, Eye, Trash2 } from "lucide-react";
import { Link } from "react-router";
import { api, unwrap, type Chapter } from "../../api/client";
import { useChapters } from "../../api/queries";
import { Badge, Button, Card, ErrorBox, IconButton, Loading, Modal, Progress, Switch, Table, Td, Th } from "../../components/ui";
import { bytes, date, relative } from "../../lib/format";
import { useToast } from "../../lib/toast";
import { eta } from "../../lib/liveProgress";
import { useListParam } from "../../lib/urlState";
import { useAccount } from "../../lib/account";

const stateTone: Record<string, "ok" | "warn" | "err" | "info" | "default" | "accent"> = {
  imported: "ok",
  missing: "warn",
  failed: "err",
  queued: "info",
  downloading: "info",
  processing: "accent",
  cleaned: "default",
};

const chaptersPerPage = 100;

/** readable: downloaded, or a source to stream it from. */
export const readable = (c: Chapter) => !!c.file || c.releases.length > 0;

const toggle = (set: Set<number>, id: number) => {
  const next = new Set(set);
  if (next.has(id)) next.delete(id);
  else next.add(id);
  return next;
};

export function ChaptersTable({ seriesId, manage = true }: { seriesId: number; manage?: boolean }) {
  const { data, isLoading, error } = useChapters(seriesId);
  const qc = useQueryClient();
  const toast = useToast();
  const { account } = useAccount();
  const tableTop = useRef<HTMLDivElement>(null);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [expanded, setExpanded] = useState<Set<number>>(new Set());
  const [explain, setExplain] = useState<Chapter | null>(null);
  const [page, setPage] = useState(1);
  const [filterParam, setFilter] = useListParam("chapters", "all");
  const filter = filterParam as "all" | "missing" | "downloaded";

  const list = useMemo(() => {
    const l = data ?? [];
    if (filter === "missing") return l.filter((c) => !c.file && c.state !== "cleaned");
    if (filter === "downloaded") return l.filter((c) => c.file);
    return l;
  }, [data, filter]);
  const pages = Math.max(1, Math.ceil(list.length / chaptersPerPage));
  const currentPage = Math.min(page, pages);
  const pageStart = (currentPage - 1) * chaptersPerPage;
  const visible = useMemo(() => list.slice(pageStart, pageStart + chaptersPerPage), [list, pageStart]);

  const refresh = useCallback(() => qc.invalidateQueries({ queryKey: ["series", seriesId] }), [qc, seriesId]);

  const monitor = useCallback(async (ids: number[], monitored: boolean) => {
    try {
      await unwrap(api.PUT("/api/v1/chapters/monitor", { body: { chapterIds: ids, monitored } }));
      refresh();
    } catch (e) {
      toast.fromError(e);
    }
  }, [refresh, toast]);
  const search = useCallback(async (ids: number[]) => {
    try {
      await unwrap(api.POST("/api/v1/series/{id}/search", { params: { path: { id: seriesId } }, body: { chapterIds: ids } }));
      toast.info(`Searching ${ids.length} chapter${ids.length === 1 ? "" : "s"}`);
    } catch (e) {
      toast.fromError(e);
    }
  }, [seriesId, toast]);
  const restore = useCallback(async (c: Chapter) => {
    try {
      await unwrap(api.POST("/api/v1/chapters/{id}/restore", { params: { path: { id: c.id } } }));
      toast.info(`Restoring chapter ${c.number}`);
      refresh();
    } catch (e) {
      toast.fromError(e);
    }
  }, [refresh, toast]);
  const mark = useCallback(async (c: Chapter, read: boolean) => {
    try {
      await unwrap(api.PUT("/api/v1/read/chapters/{id}/mark", { params: { path: { id: c.id } }, body: { read, scope: "chapter" } }));
      refresh();
    } catch (e) {
      toast.fromError(e);
    }
  }, [refresh, toast]);
  const queueAction = useCallback(async (jobID: number, action: "top" | "bottom" | "pause" | "resume") => {
    try {
      await unwrap(api.POST("/api/v1/queue/bulk", { body: { action, ids: [jobID] } }));
      refresh();
      qc.invalidateQueries({ queryKey: ["queue"] });
    } catch (e) {
      toast.fromError(e);
    }
  }, [qc, refresh, toast]);
  const queueBulk = async (action: "top" | "bottom" | "pause" | "resume" | "retry" | "remove") => {
    const ids = list.filter((chapter) => selected.has(chapter.id) && chapter.job).map((chapter) => chapter.job!.id);
    if (ids.length === 0) return;
    try {
      const result = await unwrap(api.POST("/api/v1/queue/bulk", { body: { action, ids } }));
      toast.success(`${result.affected} ${result.affected === 1 ? "queue entry" : "queue entries"} updated`);
      refresh();
      qc.invalidateQueries({ queryKey: ["queue"] });
    } catch (e) {
      toast.fromError(e);
    }
  };
  const processSelected = async () => {
    try {
      await unwrap(api.POST("/api/v1/commands", { body: { name: "ProcessExisting", body: { seriesId, chapterIds: sel } } }));
      toast.info(`Queued processing for ${sel.length} chapter${sel.length === 1 ? "" : "s"}`);
      refresh();
      qc.invalidateQueries({ queryKey: ["queue"] });
    } catch (e) {
      toast.fromError(e);
    }
  };
  const toggleSelected = useCallback((id: number) => setSelected((current) => toggle(current, id)), []);
  const toggleExpanded = useCallback((id: number) => setExpanded((current) => toggle(current, id)), []);
  const monitorOne = useCallback((id: number, monitored: boolean) => monitor([id], monitored), [monitor]);
  const searchOne = useCallback((id: number) => search([id]), [search]);
  const goToPage = useCallback((next: number) => {
    setPage(next);
    requestAnimationFrame(() => tableTop.current?.scrollIntoView({ block: "start" }));
  }, []);

  const sel = [...selected];
  const selectedJobs = list.filter((chapter) => selected.has(chapter.id) && chapter.job).length;
  return (
    <Card
      title={`Chapters (${data?.length ?? 0})`}
      actions={
        <div className="flex flex-wrap items-center gap-2">
          {sel.length > 0 && (
            <>
              <span className="text-xs text-muted">{sel.length}{" " + t("selected")}</span>
              <Button size="sm" onClick={() => monitor(sel, true)}>{t("Monitor")}</Button>
              <Button size="sm" onClick={() => monitor(sel, false)}>{t("Unmonitor")}</Button>
              <Button size="sm" icon={<Search className="size-3.5" />} onClick={() => search(sel)}>{t("Search")}</Button>
              <Button size="sm" icon={<Sparkles className="size-3.5" />} onClick={processSelected}>{t("Process")}</Button>
              {selectedJobs > 0 && (
                <>
                  <Button size="sm" icon={<ArrowUpToLine className="size-3.5" />} onClick={() => queueBulk("top")}>{t("Top")}</Button>
                  <Button size="sm" icon={<ArrowDownToLine className="size-3.5" />} onClick={() => queueBulk("bottom")}>{t("Bottom")}</Button>
                  <Button size="sm" icon={<Pause className="size-3.5" />} onClick={() => queueBulk("pause")}>{t("Pause")}</Button>
                  <Button size="sm" icon={<Play className="size-3.5" />} onClick={() => queueBulk("resume")}>{t("Resume")}</Button>
                  <Button size="sm" icon={<RotateCw className="size-3.5" />} onClick={() => queueBulk("retry")}>{t("Retry")}</Button>
                  <Button size="sm" variant="danger" icon={<Trash2 className="size-3.5" />} onClick={() => queueBulk("remove")}>{t("Remove")}</Button>
                </>
              )}
              <Button size="sm" variant="ghost" onClick={() => setSelected(new Set())}>{t("Clear")}</Button>
            </>
          )}
          <select
            className="rounded-md border border-border bg-bg px-2 py-1 text-xs"
            value={filter}
            onChange={(e) => {
              setFilter(e.target.value);
              setPage(1);
            }}
          >
            <option value="all">{t("All")}</option>
            <option value="missing">{t("Missing")}</option>
            <option value="downloaded">{t("Downloaded")}</option>
          </select>
        </div>
      }
    >
      {isLoading && <Loading />}
      {error && <ErrorBox error={error} />}
      {data && data.length === 0 && <p className="text-sm text-muted">{t("No chapters yet. Refresh the series to fetch the chapter list.")}</p>}
      {list.length > 0 && (
        <div ref={tableTop}>
          <Table className="border-0">
            <thead>
              <tr>
                <Th className="w-8">
                  <input
                    type="checkbox"
                    checked={sel.length === list.length && list.length > 0}
                    onChange={(e) => setSelected(e.target.checked ? new Set(list.map((c) => c.id)) : new Set())}
                  />
                </Th>
                <Th className="w-8" />
                <Th>#</Th>
                <Th>{t("Title")}</Th>
                <Th>{t("Released")}</Th>
                <Th>{t("State")}</Th>
                <Th>{t("File")}</Th>
                <Th className="w-48" />
              </tr>
            </thead>
            <tbody>
              {visible.map((chapter) => (
                <ChapterRow
                  key={chapter.id}
                  chapter={chapter}
                  manage={manage}
                  selected={selected.has(chapter.id)}
                  open={expanded.has(chapter.id)}
                  accountKind={account?.kind}
                  onSelect={toggleSelected}
                  onExpand={toggleExpanded}
                  onMonitor={monitorOne}
                  onSearch={searchOne}
                  onRestore={restore}
                  onMark={mark}
                  onQueueAction={queueAction}
                  onExplain={setExplain}
                />
              ))}
            </tbody>
          </Table>
        </div>
      )}
      {list.length > chaptersPerPage && (
        <div className="mt-3 flex items-center justify-end gap-3 text-sm">
          <span className="text-muted">
            {pageStart + 1}–{Math.min(pageStart + chaptersPerPage, list.length)} / {list.length} · {t("Page") + " "}{currentPage}{" " + t("of") + " "}{pages}
          </span>
          <Button size="sm" disabled={currentPage <= 1} onClick={() => goToPage(currentPage - 1)}>{t("Previous")}</Button>
          <Button size="sm" disabled={currentPage >= pages} onClick={() => goToPage(currentPage + 1)}>{t("Next")}</Button>
        </div>
      )}
      {explain && <DecisionModal seriesId={seriesId} chapter={explain} onClose={() => setExplain(null)} />}
    </Card>
  );
}

type ChapterRowProps = {
  chapter: Chapter;
  manage: boolean;
  selected: boolean;
  open: boolean;
  accountKind?: string;
  onSelect: (id: number) => void;
  onExpand: (id: number) => void;
  onMonitor: (id: number, monitored: boolean) => void;
  onSearch: (id: number) => void;
  onRestore: (chapter: Chapter) => void;
  onMark: (chapter: Chapter, read: boolean) => void;
  onQueueAction: (jobID: number, action: "top" | "bottom" | "pause" | "resume") => void;
  onExplain: (chapter: Chapter) => void;
};

/** A memoized row keeps queue progress updates from rerendering every chapter. */
const ChapterRow = memo(function ChapterRow({
  chapter: c,
  manage,
  selected,
  open,
  accountKind,
  onSelect,
  onExpand,
  onMonitor,
  onSearch,
  onRestore,
  onMark,
  onQueueAction,
  onExplain,
}: ChapterRowProps) {
  return (
    <Fragment>
      <tr className={c.monitored ? "" : "opacity-60"}>
        <Td>
          {manage && <input type="checkbox" checked={selected} onChange={() => onSelect(c.id)} />}
        </Td>
        <Td>
          {manage && <Switch checked={c.monitored} onChange={(monitored) => onMonitor(c.id, monitored)} />}
        </Td>
        <Td className="font-mono text-xs">
          {c.volume && <span className="text-muted">v{c.volume} </span>}
          {c.number}
        </Td>
        <Td>
          <div className="flex items-center gap-1">
            <button className="flex items-center gap-1 text-left hover:text-accent-2" onClick={() => onExpand(c.id)}>
              {open ? <ChevronDown className="size-3.5 shrink-0" /> : <ChevronRight className="size-3.5 shrink-0" />}
              <span className="line-clamp-1 min-w-40">{c.title || <span className="text-muted">{t("Chapter") + " "}{c.number}</span>}</span>
              <span className="text-xs text-muted">({c.releases.length})</span>
            </button>
          </div>
        </Td>
        <Td className="whitespace-nowrap text-muted">{date(c.releaseDate)}</Td>
        <Td>
          <div className="flex flex-col gap-1">
            <Badge tone={stateTone[c.state] ?? "default"}>{c.state}</Badge>
            {c.job && ["downloading", "processing", "importing"].includes(c.job.status) && (
              <div className="w-20">
                <Progress value={c.job.progress} />
              </div>
            )}
            {manage && c.job && !["completed", "failed"].includes(c.job.status) && (
              <span className="text-[11px] text-muted">{t("Priority")}: {c.job.priority}</span>
            )}
          </div>
        </Td>
        <Td className="text-xs">
          {c.file ? (
            <div className="flex flex-col">
              <span>
                {bytes(c.file.size)} · {c.file.pageCount}{t("p ·") + " "}{c.file.avgWidth}px
              </span>
              <span className="flex items-center gap-1 text-muted">
                {c.file.scanlator || c.file.sourceName}
                {c.file.upscaled && (
                  <Badge tone="accent" title={c.file.upscaleModel}>
                    <Sparkles className="size-3" />{" " + t("upscaled")}</Badge>
                )}
                {(c.file.format === "avif" || c.file.format === "jxl") && (
                  <Badge
                    tone="info"
                    title={[
                      c.file.sizeOriginal > c.file.size ? `was ${bytes(c.file.sizeOriginal)}` : "",
                      c.file.processSeconds ? `processed in ${eta(c.file.processSeconds)} (${((c.file.processPages ?? 0) / c.file.processSeconds).toFixed(1)} pages/s)` : "",
                    ]
                      .filter(Boolean)
                      .join(", ") || undefined}
                  >
                    {c.file.format}
                    {c.file.sizeOriginal > c.file.size && ` −${Math.round(100 - (100 * c.file.size) / c.file.sizeOriginal)}%`}
                  </Badge>
                )}
                {c.file.processState === "failed" && (
                  <Badge tone="err" title={c.file.processError}>{t("processing failed")}</Badge>
                )}
              </span>
            </div>
          ) : (
            <span className="text-muted">—</span>
          )}
        </Td>
        <Td className="text-right">
          <div className="flex items-center justify-end gap-1">
            {readable(c) && (
              <Link to={`/read/${c.id}`} title={c.file ? tr("Read") : tr("Read (streamed from the source)")}>
                <Button variant="primary" icon={<BookOpen className="size-4" />}>{t("Read")}</Button>
              </Link>
            )}
            {manage && c.job && !["completed", "failed"].includes(c.job.status) && (
              <>
                <IconButton title={t("Move to top")} onClick={() => onQueueAction(c.job!.id, "top")}>
                  <ArrowUpToLine className="size-4" />
                </IconButton>
                <IconButton title={t("Move to bottom")} onClick={() => onQueueAction(c.job!.id, "bottom")}>
                  <ArrowDownToLine className="size-4" />
                </IconButton>
                {c.job.status === "paused" ? (
                  <IconButton title={t("Resume")} onClick={() => onQueueAction(c.job!.id, "resume")}><Play className="size-4" /></IconButton>
                ) : (
                  <IconButton title={t("Pause")} onClick={() => onQueueAction(c.job!.id, "pause")}><Pause className="size-4" /></IconButton>
                )}
              </>
            )}
            {manage && (c.state === "cleaned" ? (
              <IconButton title={t("Restore (download again)")} onClick={() => onRestore(c)}>
                <RotateCcw className="size-4" />
              </IconButton>
            ) : (
              <IconButton title={t("Search this chapter")} onClick={() => onSearch(c.id)}>
                <Search className="size-4" />
              </IconButton>
            ))}
            {manage && (
              <IconButton title={t("Why (not) downloaded?")} onClick={() => onExplain(c)}>
                <HelpCircle className="size-4" />
              </IconButton>
            )}
          </div>
        </Td>
      </tr>
      {open && (
        <tr>
          <Td colSpan={8} className="bg-bg/60">
            {c.releases.length === 0 ? (
              <p className="text-xs text-muted">{t("No releases.")}</p>
            ) : (
              <div className="flex flex-col gap-1">
                {c.releases.map((r) => (
                  <div key={r.id} className="flex flex-wrap items-center gap-2 text-xs">
                    <Badge>{r.sourceName}</Badge>
                    <span className={r.removed ? "line-through text-muted" : ""}>{r.name}</span>
                    {r.scanlator && <span className="text-muted">{t("by") + " "}{r.scanlator}</span>}
                    <span className="text-muted">{date(r.uploadDate)}</span>
                    {r.blocklisted && <Badge tone="err">{t("blocklisted")}</Badge>}
                    {c.file?.releaseId === r.id && <Badge tone="ok">{t("current file")}</Badge>}
                    {r.webUrl && (
                      <a href={r.webUrl} target="_blank" rel="noreferrer" className="text-muted hover:text-accent-2">
                        <ExternalLink className="size-3" />
                      </a>
                    )}
                  </div>
                ))}
              </div>
            )}
            <div className="mt-3 flex flex-wrap items-center gap-2 border-t border-border pt-3">
              <span className="text-xs font-medium text-muted">{t("Read by")}</span>
              {c.readBy.map((r) => (
                <Badge key={r.readerId} tone={r.completed ? "ok" : "info"} title={r.completed ? `read ${relative(r.readAt)}` : `page ${r.page}`}>
                  <Eye className="size-3" /> {accountKind === "user" ? (r.completed ? t("Read") : `${t("Page")} ${r.page}`) : r.reader}
                </Badge>
              ))}
              {c.readBy.length === 0 && <span className="text-xs text-muted">{t("Nobody yet")}</span>}
              <span className="ml-auto flex gap-1">
                <Button size="sm" onClick={() => onMark(c, true)}>{t("Mark read")}</Button>
                <Button size="sm" onClick={() => onMark(c, false)}>{t("Mark unread")}</Button>
              </span>
            </div>
            {c.job?.error && <p className="mt-2 text-xs text-err">{t("Last error:") + " "}{c.job.error}</p>}
          </Td>
        </tr>
      )}
    </Fragment>
  );
});

function DecisionModal({ seriesId, chapter, onClose }: { seriesId: number; chapter: Chapter; onClose: () => void }) {
  const { data, error } = useQuery({
    queryKey: ["decision", seriesId, chapter.id],
    queryFn: () => unwrap(api.GET("/api/v1/series/{id}/chapters/{chapterId}/decision", { params: { path: { id: seriesId, chapterId: chapter.id } } })),
  });
  const relName = (id?: number) => chapter.releases.find((r) => r.id === id);
  return (
    <Modal open onClose={onClose} title={`Chapter ${chapter.number}: download decision`}>
      {error && <ErrorBox error={error} />}
      {!data && !error && <Loading />}
      {data && (
        <div className="flex flex-col gap-3 text-sm">
          {data.approved ? (
            <p>{t("Would download") + " "}<b>{data.approved.name}</b>{" " + t("from") + " "}<b>{data.approved.sourceName}</b>
              {data.approved.scanlator ? ` (${data.approved.scanlator})` : ""}
              {data.decision.isUpgrade ? tr(" as an upgrade") : ""}.
            </p>
          ) : (
            <p className="text-muted">{t("Nothing would be downloaded right now.")}</p>
          )}
          {data.decision.rejections.length > 0 && (
            <ul className="flex flex-col gap-1">
              {data.decision.rejections.map((r, i) => (
                <li key={i} className="flex items-start gap-2">
                  <Badge tone={r.temporary ? "warn" : "err"}>{r.temporary ? tr("temporary") : tr("rejected")}</Badge>
                  <span>
                    {r.releaseId ? <span className="text-muted">{relName(r.releaseId)?.sourceName ?? tr("release")}: </span> : null}
                    {r.reason}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </Modal>
  );
}
