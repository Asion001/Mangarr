import { Fragment, useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronDown, ChevronRight, ExternalLink, HelpCircle, RotateCcw, Search, Sparkles, Eye } from "lucide-react";
import { api, unwrap, type Chapter } from "../../api/client";
import { useChapters } from "../../api/queries";
import { Badge, Button, Card, ErrorBox, IconButton, Loading, Modal, Progress, Switch, Table, Td, Th } from "../../components/ui";
import { bytes, date, relative } from "../../lib/format";
import { useToast } from "../../lib/toast";

const stateTone: Record<string, "ok" | "warn" | "err" | "info" | "default" | "accent"> = {
  imported: "ok",
  missing: "warn",
  failed: "err",
  queued: "info",
  downloading: "info",
  processing: "accent",
  cleaned: "default",
};

export function ChaptersTable({ seriesId }: { seriesId: number }) {
  const { data, isLoading, error } = useChapters(seriesId);
  const qc = useQueryClient();
  const toast = useToast();
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [expanded, setExpanded] = useState<Set<number>>(new Set());
  const [explain, setExplain] = useState<Chapter | null>(null);
  const [filter, setFilter] = useState<"all" | "missing" | "downloaded">("all");

  const list = useMemo(() => {
    const l = data ?? [];
    if (filter === "missing") return l.filter((c) => !c.file && c.state !== "cleaned");
    if (filter === "downloaded") return l.filter((c) => c.file);
    return l;
  }, [data, filter]);

  const refresh = () => qc.invalidateQueries({ queryKey: ["series", seriesId] });

  const monitor = async (ids: number[], monitored: boolean) => {
    try {
      await unwrap(api.PUT("/api/v1/chapters/monitor", { body: { chapterIds: ids, monitored } }));
      refresh();
    } catch (e) {
      toast.fromError(e);
    }
  };
  const search = async (ids: number[]) => {
    try {
      await unwrap(api.POST("/api/v1/series/{id}/search", { params: { path: { id: seriesId } }, body: { chapterIds: ids } }));
      toast.info(`Searching ${ids.length} chapter${ids.length === 1 ? "" : "s"}`);
    } catch (e) {
      toast.fromError(e);
    }
  };
  const restore = async (c: Chapter) => {
    try {
      await unwrap(api.POST("/api/v1/chapters/{id}/restore", { params: { path: { id: c.id } } }));
      toast.info(`Restoring chapter ${c.number}`);
      refresh();
    } catch (e) {
      toast.fromError(e);
    }
  };
  const toggle = (set: Set<number>, id: number) => {
    const n = new Set(set);
    if (n.has(id)) n.delete(id);
    else n.add(id);
    return n;
  };

  const sel = [...selected];
  return (
    <Card
      title={`Chapters (${data?.length ?? 0})`}
      actions={
        <div className="flex flex-wrap items-center gap-2">
          {sel.length > 0 && (
            <>
              <span className="text-xs text-muted">{sel.length} selected</span>
              <Button size="sm" onClick={() => monitor(sel, true)}>
                Monitor
              </Button>
              <Button size="sm" onClick={() => monitor(sel, false)}>
                Unmonitor
              </Button>
              <Button size="sm" icon={<Search className="size-3.5" />} onClick={() => search(sel)}>
                Search
              </Button>
              <Button size="sm" variant="ghost" onClick={() => setSelected(new Set())}>
                Clear
              </Button>
            </>
          )}
          <select className="rounded-md border border-border bg-bg px-2 py-1 text-xs" value={filter} onChange={(e) => setFilter(e.target.value as typeof filter)}>
            <option value="all">All</option>
            <option value="missing">Missing</option>
            <option value="downloaded">Downloaded</option>
          </select>
        </div>
      }
    >
      {isLoading && <Loading />}
      {error && <ErrorBox error={error} />}
      {data && data.length === 0 && <p className="text-sm text-muted">No chapters yet. Refresh the series to fetch the chapter list.</p>}
      {list.length > 0 && (
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
              <Th>Title</Th>
              <Th>Released</Th>
              <Th>State</Th>
              <Th>File</Th>
              <Th>Read by</Th>
              <Th className="w-28" />
            </tr>
          </thead>
          <tbody>
            {list.map((c) => {
              const open = expanded.has(c.id);
              return (
                <Fragment key={c.id}>
                  <tr className={c.monitored ? "" : "opacity-60"}>
                    <Td>
                      <input type="checkbox" checked={selected.has(c.id)} onChange={() => setSelected(toggle(selected, c.id))} />
                    </Td>
                    <Td>
                      <Switch checked={c.monitored} onChange={(v) => monitor([c.id], v)} />
                    </Td>
                    <Td className="font-mono text-xs">
                      {c.volume && <span className="text-muted">v{c.volume} </span>}
                      {c.number}
                    </Td>
                    <Td>
                      <button className="flex items-center gap-1 text-left hover:text-accent-2" onClick={() => setExpanded(toggle(expanded, c.id))}>
                        {open ? <ChevronDown className="size-3.5 shrink-0" /> : <ChevronRight className="size-3.5 shrink-0" />}
                        <span className="line-clamp-1 min-w-40">{c.title || <span className="text-muted">Chapter {c.number}</span>}</span>
                        <span className="text-xs text-muted">({c.releases.length})</span>
                      </button>
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
                      </div>
                    </Td>
                    <Td className="text-xs">
                      {c.file ? (
                        <div className="flex flex-col">
                          <span>
                            {bytes(c.file.size)} · {c.file.pageCount}p · {c.file.avgWidth}px
                          </span>
                          <span className="flex items-center gap-1 text-muted">
                            {c.file.scanlator || c.file.sourceName}
                            {c.file.upscaled && (
                              <Badge tone="accent" title={c.file.upscaleModel}>
                                <Sparkles className="size-3" /> upscaled
                              </Badge>
                            )}
                          </span>
                        </div>
                      ) : (
                        <span className="text-muted">—</span>
                      )}
                    </Td>
                    <Td>
                      <div className="flex flex-wrap gap-1">
                        {c.readBy.map((r) => (
                          <Badge key={r.readerId} tone={r.completed ? "ok" : "info"} title={r.completed ? `read ${relative(r.readAt)}` : `page ${r.page}`}>
                            <Eye className="size-3" /> {r.reader}
                          </Badge>
                        ))}
                      </div>
                    </Td>
                    <Td className="text-right">
                      <div className="flex justify-end">
                        {c.state === "cleaned" ? (
                          <IconButton title="Restore (download again)" onClick={() => restore(c)}>
                            <RotateCcw className="size-4" />
                          </IconButton>
                        ) : (
                          <IconButton title="Search this chapter" onClick={() => search([c.id])}>
                            <Search className="size-4" />
                          </IconButton>
                        )}
                        <IconButton title="Why (not) downloaded?" onClick={() => setExplain(c)}>
                          <HelpCircle className="size-4" />
                        </IconButton>
                      </div>
                    </Td>
                  </tr>
                  {open && (
                    <tr>
                      <Td colSpan={9} className="bg-bg/60">
                        {c.releases.length === 0 ? (
                          <p className="text-xs text-muted">No releases.</p>
                        ) : (
                          <div className="flex flex-col gap-1">
                            {c.releases.map((r) => (
                              <div key={r.id} className="flex flex-wrap items-center gap-2 text-xs">
                                <Badge>{r.sourceName}</Badge>
                                <span className={r.removed ? "line-through text-muted" : ""}>{r.name}</span>
                                {r.scanlator && <span className="text-muted">by {r.scanlator}</span>}
                                <span className="text-muted">{date(r.uploadDate)}</span>
                                {r.blocklisted && <Badge tone="err">blocklisted</Badge>}
                                {c.file?.releaseId === r.id && <Badge tone="ok">current file</Badge>}
                                {r.webUrl && (
                                  <a href={r.webUrl} target="_blank" rel="noreferrer" className="text-muted hover:text-accent-2">
                                    <ExternalLink className="size-3" />
                                  </a>
                                )}
                              </div>
                            ))}
                          </div>
                        )}
                        {c.job?.error && <p className="mt-2 text-xs text-err">Last error: {c.job.error}</p>}
                      </Td>
                    </tr>
                  )}
                </Fragment>
              );
            })}
          </tbody>
        </Table>
      )}
      {explain && <DecisionModal seriesId={seriesId} chapter={explain} onClose={() => setExplain(null)} />}
    </Card>
  );
}

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
            <p>
              Would download <b>{data.approved.name}</b> from <b>{data.approved.sourceName}</b>
              {data.approved.scanlator ? ` (${data.approved.scanlator})` : ""}
              {data.decision.isUpgrade ? " as an upgrade" : ""}.
            </p>
          ) : (
            <p className="text-muted">Nothing would be downloaded right now.</p>
          )}
          {data.decision.rejections.length > 0 && (
            <ul className="flex flex-col gap-1">
              {data.decision.rejections.map((r, i) => (
                <li key={i} className="flex items-start gap-2">
                  <Badge tone={r.temporary ? "warn" : "err"}>{r.temporary ? "temporary" : "rejected"}</Badge>
                  <span>
                    {r.releaseId ? <span className="text-muted">{relName(r.releaseId)?.sourceName ?? "release"}: </span> : null}
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
