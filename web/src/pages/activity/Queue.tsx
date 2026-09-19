import { t as tr, t } from "../../lib/i18n/core";
import { useMemo, useState } from "react";
import { Link } from "react-router";
import { useQueryClient } from "@tanstack/react-query";
import { ArrowDownToLine, ArrowUpToLine, Ban, Eraser, Pause, Play, RotateCw, Trash2 } from "lucide-react";
import { api, unwrap, type Job } from "../../api/client";
import { useQueue } from "../../api/queries";
import { Badge, Button, Confirm, EmptyState, ErrorBox, IconButton, Input, Loading, PageHeader, Progress, Select, Switch, Table, Td, Th } from "../../components/ui";
import { relative } from "../../lib/format";
import { useListParam, useQueryParam } from "../../lib/urlState";
import { useToast } from "../../lib/toast";
import { describe, useLiveProgress } from "../../lib/liveProgress";

const tone = (s: string) => (s === "completed" ? "ok" : s === "failed" ? "err" : s === "paused" ? "warn" : s === "queued" ? "default" : "info");
const statuses = ["downloading", "processing", "importing", "queued", "paused", "failed", "completed"] as const;
type Action = "pause" | "resume" | "retry" | "remove" | "blocklist" | "top" | "bottom";

export function QueuePage({ mode }: { mode: "downloads" | "processing" }) {
  const qc = useQueryClient();
  const toast = useToast();
  const [status, setStatus] = useListParam("status");
  const [q, setQ] = useQueryParam("q");
  const [pageStr, setPage] = useListParam("page", "1");
  const [group, setGroup] = useListParam("group");
  const page = Number(pageStr) || 1;
  const pageSize = mode === "processing" ? 500 : 100;
  const filter = { status: status ? status.split(",") : undefined, kind: mode === "processing" ? "reprocess" as const : "download" as const, q: q || undefined, includeDone: true };
  const { data, isLoading, error } = useQueue({ ...filter, page, pageSize });
  const liveMap = useLiveProgress();
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [allMatching, setAllMatching] = useState(false);
  const [lastClicked, setLastClicked] = useState<number | null>(null);
  const [confirm, setConfirm] = useState<{ action: Action; label: string } | null>(null);
  const items = data?.items ?? [];
  const total = data?.total ?? 0;
  const state = data?.state;

  const refresh = () => qc.invalidateQueries({ queryKey: ["queue"] });
  const resetSelection = () => (setSelected(new Set()), setAllMatching(false));
  const run = async (action: Action, ids?: number[]) => {
    try {
      const body = allMatching && !ids ? { action, filter: { statuses: filter.status, kind: filter.kind, q: filter.q, includeDone: true } } : { action, ids: ids ?? [...selected] };
      const r = await unwrap(api.POST("/api/v1/queue/bulk", { body }));
      toast.success(`${r.affected} ${r.affected === 1 ? "entry" : "entries"} updated`);
      if (!ids) resetSelection();
      refresh();
    } catch (e) {
      toast.fromError(e);
    }
  };
  const pauseAll = async (minutes?: number) => {
    try {
      await unwrap(minutes === undefined ? api.POST("/api/v1/queue/resume") : api.POST("/api/v1/queue/pause", { body: { minutes } }));
      refresh();
    } catch (e) {
      toast.fromError(e);
    }
  };
  const clear = async () => {
    await unwrap(api.POST("/api/v1/queue/clear-finished"));
    refresh();
  };

  // click selects one row; shift+click selects the range from the last click
  const toggle = (idx: number, shift: boolean) => {
    setAllMatching(false);
    const id = items[idx].id;
    setSelected((cur) => {
      const next = new Set(cur);
      if (shift && lastClicked !== null) {
        const [a, b] = [Math.min(lastClicked, idx), Math.max(lastClicked, idx)];
        const on = !cur.has(id);
        for (let i = a; i <= b; i++) on ? next.add(items[i].id) : next.delete(items[i].id);
      } else if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
    setLastClicked(idx);
  };
  const pageAllSelected = items.length > 0 && items.every((j) => selected.has(j.id));
  const count = allMatching ? total : selected.size;

  const groups = useMemo(() => {
    if (!group) return null;
    const m = new Map<number, { title: string; jobs: { job: Job; idx: number }[] }>();
    items.forEach((job, idx) => {
      const g = m.get(job.seriesId) ?? { title: job.seriesTitle, jobs: [] };
      g.jobs.push({ job, idx });
      m.set(job.seriesId, g);
    });
    return [...m.entries()];
  }, [items, group]);

  const row = (j: Job, idx: number) => {
    const live = j.status === "completed" || j.status === "failed" ? undefined : (liveMap.get(j.id) ?? j.live);
    return (
    <tr key={j.id} className={selected.has(j.id) || allMatching ? "bg-accent/5" : undefined}>
      <Td className="w-8">
        <input type="checkbox" aria-label={t("Select")} checked={allMatching || selected.has(j.id)} onChange={() => undefined} onClick={(e) => toggle(idx, e.shiftKey)} />
      </Td>
      <Td className="w-14 font-mono text-xs text-muted">{(page - 1) * pageSize + idx + 1}</Td>
      {!group && (
        <Td>
          <Link to={`/series/${j.seriesId}`} className="font-medium hover:text-accent-2">
            {j.seriesTitle}
          </Link>
        </Td>
      )}
      <Td>
        {j.chapter}
        {j.isUpgrade && (
          <span className="ml-1.5">
            <Badge tone="info">{t("upgrade")}</Badge>
          </span>
        )}
        {j.kind === "reprocess" && (
          <span className="ml-1.5">
            <Badge tone="accent">{t("process")}</Badge>
          </span>
        )}
        {j.priority !== 0 && <span className="ml-1.5 text-xs text-muted">{j.priority > 0 ? "↑" : "↓"}</span>}
      </Td>
      <Td className="text-muted">
        {j.sourceName}
        {j.scanlator && ` · ${j.scanlator}`}
      </Td>
      <Td>
        <Badge tone={tone(j.status)}>{j.status}</Badge>
        {j.attempt > 0 && j.status !== "completed" && <span className="ml-1 text-xs text-muted">{t("try") + " "}{j.attempt + 1}</span>}
        {j.worker && (
          <Badge tone="info" title={t("Being done on this worker")}>
            {j.worker}
          </Badge>
        )}
        {j.error && (
          <div className="mt-1 max-w-sm truncate text-xs text-err" title={j.error}>
            {j.error}
          </div>
        )}
      </Td>
      <Td className="w-48">
        {live ? (
          <>
            <Progress value={live.total > 0 ? (live.done / live.total) * 100 : 0} tone="accent" />
            <div className="mt-1 text-xs text-muted">{describe(live)}</div>
          </>
        ) : (
          <>
            <Progress value={j.progress} tone={j.status === "failed" ? "err" : j.status === "completed" ? "ok" : "accent"} />
            <div className="mt-1 text-xs text-muted">
              {j.pagesDone}/{j.pagesTotal}{" " + t("pages")}</div>
          </>
        )}
      </Td>
      <Td className="whitespace-nowrap text-muted">{relative(j.updatedAt)}</Td>
      <Td className="text-right">
        <div className="flex justify-end">
          {j.status === "failed" && (
            <IconButton title={t("Retry")} onClick={() => run("retry", [j.id])}>
              <RotateCw className="size-4" />
            </IconButton>
          )}
          {j.status === "paused" ? (
            <IconButton title={t("Resume")} onClick={() => run("resume", [j.id])}>
              <Play className="size-4" />
            </IconButton>
          ) : (
            ["queued", "downloading", "processing"].includes(j.status) && (
              <IconButton title={t("Pause")} onClick={() => run("pause", [j.id])}>
                <Pause className="size-4" />
              </IconButton>
            )
          )}
          {j.status !== "completed" && (
            <>
              <IconButton title={t("Move to top")} onClick={() => run("top", [j.id])}>
                <ArrowUpToLine className="size-4" />
              </IconButton>
              <IconButton title={t("Move to bottom")} onClick={() => run("bottom", [j.id])}>
                <ArrowDownToLine className="size-4" />
              </IconButton>
              <IconButton title={t("Remove")} onClick={() => (setSelected(new Set([j.id])), setAllMatching(false), setConfirm({ action: "remove", label: "Remove" }))}>
                <Trash2 className="size-4" />
              </IconButton>
            </>
          )}
        </div>
      </Td>
    </tr>
    );
  };

  return (
    <>
      <PageHeader
        title={mode === "processing" ? t("Processing queue") : t("Download queue")}
        subtitle={mode === "processing" ? t("Every chapter waiting to be upscaled or encoded, in processing order") : t("Chapters waiting to be downloaded and imported, in download order")}
        actions={
          <>
            {state?.paused ? (
              <Button variant="primary" icon={<Play className="size-4" />} onClick={() => pauseAll()}>{t("Resume queue")}{state.pausedUntil ? ` (paused until ${new Date(state.pausedUntil).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })})` : ""}
              </Button>
            ) : (
              <Select className="w-44" value="" onChange={(e) => e.target.value && pauseAll(Number(e.target.value))} title={t("Pause the whole queue")}>
                <option value="">{t("Pause queue…")}</option>
                <option value="60">{t("for 1 hour")}</option>
                <option value="360">{t("for 6 hours")}</option>
                <option value="1440">{t("for 24 hours")}</option>
                <option value="0">{t("until resumed")}</option>
              </Select>
            )}
            <Button icon={<Eraser className="size-4" />} onClick={clear}>{t("Clear finished")}</Button>
          </>
        }
      />
      {state?.quiet?.windows?.length ? (
        <p className="mb-3 text-sm text-warn">{t("Quiet hours (")}{state.quiet.windows.join(", ")}):{" "}
          {[state.quiet.pauseDownloads && "downloads paused", state.quiet.pauseProcessing && "processing paused", state.quiet.throttle && `${state.quiet.throttle} throttling`]
            .filter(Boolean)
            .join(", ")}
        </p>
      ) : null}
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <button
          onClick={() => (setStatus(""), setPage("1"), resetSelection())}
          className={`rounded-full border px-3 py-1 text-xs ${!status ? "border-accent bg-accent/15 text-fg" : "border-border text-muted hover:text-fg"}`}
          title={t("Show every status")}
        >{t("all") + " "}{Object.values(data?.counts ?? {}).reduce((a, b) => a + b, 0)}
        </button>
        {statuses.map((s) => {
          const n = data?.counts?.[s] ?? 0;
          const on = status.split(",").includes(s);
          if (!n && !on) return null;
          return (
            <button
              key={s}
              onClick={() => (setStatus(on ? status.split(",").filter((x) => x !== s && x).join(",") : [...status.split(",").filter(Boolean), s].join(",")), setPage("1"), resetSelection())}
              className={`rounded-full border px-3 py-1 text-xs ${on ? "border-accent bg-accent/15 text-fg" : "border-border text-muted hover:text-fg"}`}
            >
              {s} {n}
            </button>
          );
        })}
        <Input className="max-w-xs" placeholder={t("Filter by series…")} defaultValue={q} onChange={(e) => (setQ(e.target.value), setPage("1"), resetSelection())} />
        <Switch checked={!!group} onChange={(v) => setGroup(v ? "series" : "")} label={t("Group by series")} />
      </div>
      {count > 0 && (
        <div className="sticky top-0 z-10 mb-3 flex flex-wrap items-center gap-2 rounded-lg border border-accent/40 bg-panel p-2 text-sm shadow">
          <span className="font-medium">{count}{" " + t("selected")}</span>
          {pageAllSelected && !allMatching && total > items.length && (
            <button className="text-accent-2 hover:underline" onClick={() => setAllMatching(true)}>{t("Select all") + " "}{total}{" " + t("matching")}</button>
          )}
          <div className="ml-auto flex flex-wrap gap-1">
            <Button size="sm" icon={<Pause className="size-3.5" />} onClick={() => run("pause")}>{t("Pause")}</Button>
            <Button size="sm" icon={<Play className="size-3.5" />} onClick={() => run("resume")}>{t("Resume")}</Button>
            <Button size="sm" icon={<RotateCw className="size-3.5" />} onClick={() => run("retry")}>{t("Retry")}</Button>
            <Button size="sm" icon={<ArrowUpToLine className="size-3.5" />} onClick={() => run("top")}>{t("Top")}</Button>
            <Button size="sm" icon={<ArrowDownToLine className="size-3.5" />} onClick={() => run("bottom")}>{t("Bottom")}</Button>
            <Button size="sm" icon={<Ban className="size-3.5" />} onClick={() => setConfirm({ action: "blocklist", label: "Remove and blocklist" })}>{t("Blocklist")}</Button>
            <Button size="sm" variant="danger" icon={<Trash2 className="size-3.5" />} onClick={() => setConfirm({ action: "remove", label: "Remove" })}>{t("Remove")}</Button>
            <Button size="sm" variant="ghost" onClick={resetSelection}>{t("Clear")}</Button>
          </div>
        </div>
      )}
      {isLoading && <Loading />}
      {error && <ErrorBox error={error} />}
      {data && total === 0 && <EmptyState title={t("Queue is empty")}>{t("New chapters are queued automatically when a monitored series gets an update.")}</EmptyState>}
      {items.length > 0 && (
        <div className="overflow-x-auto">
          <Table>
            <thead>
              <tr>
                <Th className="w-8">
                  <input
                    type="checkbox"
                    aria-label={t("Select page")}
                    checked={pageAllSelected || allMatching}
                    onChange={() => (pageAllSelected ? resetSelection() : setSelected(new Set(items.map((j) => j.id))))}
                  />
                </Th>
                <Th className="w-14">{t("Order")}</Th>
                {!group && <Th>{t("Series")}</Th>}
                <Th>{t("Chapter")}</Th>
                <Th>{t("Source")}</Th>
                <Th>{t("Status")}</Th>
                <Th>{t("Progress")}</Th>
                <Th>{t("Updated")}</Th>
                <Th />
              </tr>
            </thead>
            <tbody>
              {groups
                ? groups.map(([sid, g]) => (
                    <SeriesGroup
                      key={sid}
                      seriesId={sid}
                      title={g.title}
                      count={g.jobs.length}
                      selected={g.jobs.every(({ job }) => selected.has(job.id))}
                      onSelect={(on) =>
                        setSelected((cur) => {
                          const next = new Set(cur);
                          g.jobs.forEach(({ job }) => (on ? next.add(job.id) : next.delete(job.id)));
                          return next;
                        })
                      }
                    >
                      {g.jobs.map(({ job, idx }) => row(job, idx))}
                    </SeriesGroup>
                  ))
                : items.map((j, i) => row(j, i))}
            </tbody>
          </Table>
        </div>
      )}
      {total > pageSize && (
        <div className="mt-4 flex items-center justify-center gap-3 text-sm">
          <Button size="sm" disabled={page <= 1} onClick={() => setPage(String(page - 1))}>{t("Previous")}</Button>
          <span className="text-muted">{t("Page") + " "}{page}{" " + t("of") + " "}{Math.ceil(total / pageSize)}
          </span>
          <Button size="sm" disabled={page * pageSize >= total} onClick={() => setPage(String(page + 1))}>{t("Next")}</Button>
        </div>
      )}
      <Confirm
        open={!!confirm}
        title={confirm?.label ?? ""}
        danger
        confirmLabel={confirm?.label}
        message={
          confirm?.action === "blocklist"
            ? `Remove ${count} entries and blocklist their releases? Other sources may be tried.`
            : `Remove ${count} ${count === 1 ? "entry" : "entries"} from the queue?`
        }
        onConfirm={async () => {
          if (confirm) await run(confirm.action);
          setConfirm(null);
        }}
        onClose={() => setConfirm(null)}
      />
    </>
  );
}

function SeriesGroup({
  seriesId,
  title,
  count,
  selected,
  onSelect,
  children,
}: {
  seriesId: number;
  title: string;
  count: number;
  selected: boolean;
  onSelect: (on: boolean) => void;
  children: React.ReactNode;
}) {
  const [open, setOpen] = useState(true);
  return (
    <>
      <tr className="bg-panel-2/60">
        <Td className="w-8">
          <input type="checkbox" aria-label={`Select ${title}`} checked={selected} onChange={(e) => onSelect(e.target.checked)} />
        </Td>
        <Td colSpan={7}>
          <button className="mr-2 text-muted" onClick={() => setOpen(!open)} aria-label={open ? tr("Collapse") : tr("Expand")}>
            {open ? "▾" : "▸"}
          </button>
          <Link to={`/series/${seriesId}`} className="font-medium hover:text-accent-2">
            {title}
          </Link>
          <span className="ml-2 text-xs text-muted">{count}{" " + t("chapters")}</span>
        </Td>
      </tr>
      {open && children}
    </>
  );
}
