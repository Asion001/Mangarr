import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router";
import { AlertTriangle, CheckCircle2, Info, LifeBuoy, RefreshCw, XCircle } from "lucide-react";
import { api, apiUrl, unwrap } from "../../api/client";
import { useCommands, useHealth } from "../../api/queries";
import { Badge, Button, Card, Loading, PageHeader, Progress, Table, Td, Th } from "../../components/ui";
import { bytes, dateTime, duration, relative } from "../../lib/format";
import { useToast } from "../../lib/toast";
import { describe, eta, useLiveProgress } from "../../lib/liveProgress";

export function StatusPage() {
  const qc = useQueryClient();
  const { data: health, isLoading } = useHealth();
  const { data: status } = useQuery({ queryKey: ["system-status"], queryFn: () => unwrap(api.GET("/api/v1/system/status")) });
  const { data: commands } = useCommands();
  const run = async () => {
    const r = await unwrap(api.POST("/api/v1/health/check"));
    qc.setQueryData(["health"], r);
  };
  const icon = (t: string) =>
    t === "error" ? <XCircle className="size-4 text-err" /> : t === "warning" ? <AlertTriangle className="size-4 text-warn" /> : <Info className="size-4 text-info" />;
  return (
    <>
      <PageHeader
        title="Status"
        actions={
          <a href={apiUrl("api/v1/system/diagnostics")} download title="Status, health, modules, settings, queue and logs, with secrets removed">
            <Button icon={<LifeBuoy className="size-4" />}>Download diagnostics</Button>
          </a>
        }
      />
      <Card
        title="Health"
        className="mb-6"
        actions={
          <Button size="sm" icon={<RefreshCw className="size-3.5" />} onClick={run}>
            Check now
          </Button>
        }
      >
        {isLoading && <Loading />}
        {health && health.checks.length === 0 && (
          <p className="flex items-center gap-2 text-sm text-ok">
            <CheckCircle2 className="size-4" /> Everything looks good.
          </p>
        )}
        <div className="flex flex-col gap-2">
          {health?.checks.map((c, i) => (
            <div key={i} className="flex items-start gap-2 text-sm">
              {icon(c.type)}
              <span className="font-medium">{c.source}:</span>
              <span className="text-fg/85">{c.message}</span>
            </div>
          ))}
        </div>
        {health && <p className="mt-3 text-xs text-muted">Checked {relative(health.checkedAt)}</p>}
      </Card>
      {status && (
        <Card title="About" className="mb-6">
          <dl className="grid grid-cols-[140px_1fr] gap-y-1.5 text-sm">
            <dt className="text-muted">Version</dt>
            <dd>
              {status.version} <span className="text-muted">({status.commit})</span>
            </dd>
            <dt className="text-muted">Runtime</dt>
            <dd>
              {status.goVersion} · {status.os}/{status.arch}
            </dd>
            <dt className="text-muted">Database</dt>
            <dd>{status.database}</dd>
            <dt className="text-muted">Data folder</dt>
            <dd className="font-mono text-xs">{status.dataDir}</dd>
            <dt className="text-muted">Started</dt>
            <dd>{dateTime(status.startedAt)}</dd>
          </dl>
        </Card>
      )}
      <ProcessingCard />
      <CacheCard />
      <Card title="Recent commands">
        <Table className="border-0">
          <thead>
            <tr>
              <Th>Command</Th>
              <Th>Status</Th>
              <Th>Message</Th>
              <Th>Trigger</Th>
              <Th>Queued</Th>
              <Th>Duration</Th>
            </tr>
          </thead>
          <tbody>
            {commands?.map((c) => (
              <tr key={c.id}>
                <Td className="font-medium">{c.name}</Td>
                <Td>
                  <Badge tone={c.status === "completed" ? "ok" : c.status === "failed" ? "err" : c.status === "started" ? "info" : "default"}>{c.status}</Badge>
                </Td>
                <Td className="max-w-md truncate text-xs text-muted" >{c.error || c.message}</Td>
                <Td className="text-xs text-muted">{c.trigger}</Td>
                <Td className="whitespace-nowrap text-xs text-muted">{relative(c.queuedAt)}</Td>
                <Td className="text-xs text-muted">{duration(c.durationMs)}</Td>
              </tr>
            ))}
          </tbody>
        </Table>
      </Card>
    </>
  );
}

function CacheCard() {
  const qc = useQueryClient();
  const toast = useToast();
  const { data } = useQuery({ queryKey: ["cache"], queryFn: () => unwrap(api.GET("/api/v1/system/cache")) });
  const clear = async (body: { catalogs?: boolean; images?: string[] }) => {
    try {
      qc.setQueryData(["cache"], await unwrap(api.POST("/api/v1/system/cache/clear", { body: { catalogs: false, ...body } })));
      toast.success("Cache cleared");
    } catch (e) {
      toast.fromError(e);
    }
  };
  const compact = async () => {
    try {
      await unwrap(api.POST("/api/v1/commands", { body: { name: "CompactImageCache" } }));
      toast.info("Resizing cached images");
    } catch (e) {
      toast.fromError(e);
    }
  };
  if (!data) return null;
  const pct = data.imageMaxBytes > 0 ? Math.min(100, (data.imageBytes / data.imageMaxBytes) * 100) : 0;
  return (
    <Card
      title="Caches"
      className="mb-6"
      actions={
        <Button size="sm" onClick={compact} title="Resize cached thumbnails and covers to small JPEGs">
          Compact images
        </Button>
      }
    >
      <div className="mb-3 flex flex-col gap-1 text-sm">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <span>
            Images on disk: <b>{bytes(data.imageBytes)}</b>
            {data.imageMaxBytes > 0 ? ` of ${bytes(data.imageMaxBytes)} limit` : " (no limit)"}
          </span>
          <span className="text-xs text-muted">oldest images are removed above the limit (Settings → General)</span>
        </div>
        {data.imageMaxBytes > 0 && <Progress value={pct} tone={pct > 95 ? "warn" : "accent"} />}
        {data.needsCompact && <p className="text-xs text-warn">Images cached by an older version are being resized; sizes drop once that finishes.</p>}
      </div>
      <div className="overflow-x-auto">
        <Table>
          <thead>
            <tr>
              <Th>Cache</Th>
              <Th>Entries</Th>
              <Th>Size</Th>
              <Th>Average</Th>
              <Th></Th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <Td>Search results & manga details (memory)</Td>
              <Td>{data.entries}</Td>
              <Td>
                {bytes(data.bytes)} / {bytes(data.maxBytes)}
              </Td>
              <Td>—</Td>
              <Td className="text-right">
                <Button size="sm" onClick={() => clear({ catalogs: true })}>
                  Clear
                </Button>
              </Td>
            </tr>
            {data.images.map((b) => (
              <tr key={b.name}>
                <Td>{imageLabels[b.name] ?? b.name}</Td>
                <Td>{b.files}</Td>
                <Td>{bytes(b.bytes)}</Td>
                <Td>{b.files > 0 ? bytes(b.bytes / b.files) : "—"}</Td>
                <Td className="text-right">
                  <Button size="sm" onClick={() => clear({ images: [b.name] })}>
                    Clear
                  </Button>
                </Td>
              </tr>
            ))}
          </tbody>
        </Table>
      </div>
    </Card>
  );
}

const imageLabels: Record<string, string> = { thumbs: "Search thumbnails (disk)", assets: "Extension icons (disk)", covers: "Series covers (disk)" };

function ProcessingCard() {
  const qc = useQueryClient();
  const toast = useToast();
  const liveMap = useLiveProgress();
  const { data } = useQuery({
    queryKey: ["processing"],
    queryFn: () => unwrap(api.GET("/api/v1/processing")),
    refetchInterval: (q) => ((q.state.data?.active.length ?? 0) > 0 || (q.state.data?.pending ?? 0) > 0 ? 10_000 : 60_000),
  });
  if (!data) return null;
  const resume = async () => {
    try {
      await unwrap(api.POST("/api/v1/processing/resume"));
      qc.invalidateQueries({ queryKey: ["processing"] });
      toast.success("Re-encoding resumed");
    } catch (e) {
      toast.fromError(e);
    }
  };
  return (
    <Card title="Processing" className="mb-6">
      {data.state.encodeBlocked && (
        <div className="mb-3 flex flex-wrap items-center gap-2 rounded-md border border-err/40 bg-err/10 p-2 text-sm">
          <span className="flex-1">Re-encoding is paused: {data.state.reason}</span>
          <Button size="sm" onClick={resume}>
            Resume
          </Button>
        </div>
      )}
      <div className="grid grid-cols-2 gap-3 text-sm sm:grid-cols-5">
        <Stat label="Space saved" value={bytes(data.spaceSaved)} />
        <Stat label="Processed" value={String(data.processed)} />
        <Stat
          label="Waiting"
          value={data.pending > 0 ? `${data.pending} ch · ${data.pendingPages.toLocaleString()} p` : "0"}
          hint={data.failed > 0 ? `${data.failed} gave up` : undefined}
        />
        <Stat label="Speed (last day)" value={data.pagesPerMinute > 0 ? `${data.pagesPerMinute.toFixed(1)} pages/min` : "—"} />
        <Stat label="Backlog done in" value={data.etaSeconds > 0 ? `~${eta(data.etaSeconds)}` : "—"} />
      </div>

      {data.active.length > 0 && (
        <div className="mt-4 flex flex-col gap-2">
          {data.active.map((j) => {
            const live = liveMap.get(j.id) ?? j.live;
            return (
              <div key={j.id} className="rounded-md border border-border p-2 text-sm">
                <div className="mb-1 flex flex-wrap justify-between gap-2">
                  <Link to={`/series/${j.seriesId}`} className="font-medium hover:text-accent-2">
                    {j.seriesTitle} · ch. {j.chapter}
                  </Link>
                  <span className="text-xs text-muted">{live ? describe(live) : j.status}</span>
                </div>
                <Progress value={live && live.total > 0 ? (live.done / live.total) * 100 : 0} />
              </div>
            );
          })}
        </div>
      )}

      <SavedChart />

      {data.recent.length > 0 && (
        <div className="mt-4 overflow-x-auto">
          <Table>
            <thead>
              <tr>
                <Th>Recently processed</Th>
                <Th>Size</Th>
                <Th>Pages</Th>
                <Th>Time</Th>
                <Th>When</Th>
              </tr>
            </thead>
            <tbody>
              {data.recent.map((f, i) => (
                <tr key={i}>
                  <Td>
                    <Link to={`/series/${f.seriesId}`} className="hover:text-accent-2">
                      {f.seriesTitle} · ch. {f.chapter}
                    </Link>
                  </Td>
                  <Td className="whitespace-nowrap">
                    {bytes(f.sizeOriginal)} → {bytes(f.size)}
                    {f.sizeOriginal > 0 && <span className="ml-1 text-xs text-ok">−{Math.round((1 - f.size / f.sizeOriginal) * 100)}%</span>}
                  </Td>
                  <Td>{f.pages}</Td>
                  <Td className="whitespace-nowrap">
                    {eta(f.seconds)}
                    {f.seconds > 0 && <span className="ml-1 text-xs text-muted">{(f.pages / f.seconds).toFixed(1)} p/s</span>}
                  </Td>
                  <Td className="whitespace-nowrap text-muted">{relative(f.processedAt)}</Td>
                </tr>
              ))}
            </tbody>
          </Table>
        </div>
      )}
      <p className="mt-3 text-xs text-muted">
        Encoders: {data.engines.map((e) => `${e.name} (${e.format}${e.slow ? ", slow" : ""})`).join(", ") || "none"}
      </p>
    </Card>
  );
}

function Stat({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <div>
      <div className="text-muted">{label}</div>
      <div className="font-medium">{value}</div>
      {hint && <div className="text-xs text-warn">{hint}</div>}
    </div>
  );
}

/** SavedChart shows MB saved (bars) and pages processed per day for 30 days. */
function SavedChart() {
  const { data } = useQuery({ queryKey: ["processing", "history"], queryFn: () => unwrap(api.GET("/api/v1/processing/history", { params: { query: { days: 30 } } })) });
  if (!data || !data.some((d) => d.files > 0)) return null;
  const saved = data.map((d) => Math.max(0, d.bytesBefore - d.bytesAfter));
  const maxSaved = Math.max(...saved, 1);
  const total = saved.reduce((a, b) => a + b, 0);
  const pages = data.reduce((a, d) => a + d.pages, 0);
  const W = 600,
    H = 120,
    gap = 2,
    bw = W / data.length - gap;
  return (
    <div className="mt-4">
      <div className="mb-1 flex flex-wrap justify-between gap-2 text-xs text-muted">
        <span>Saved per day, last 30 days</span>
        <span>
          {bytes(total)} saved · {pages.toLocaleString()} pages
        </span>
      </div>
      <svg viewBox={`0 0 ${W} ${H + 14}`} className="h-auto w-full" role="img" aria-label="Space saved per day">
        {data.map((d, i) => {
          const h = (saved[i] / maxSaved) * H;
          const x = i * (bw + gap);
          return (
            <g key={d.day}>
              <rect x={x} y={H - h} width={bw} height={Math.max(h, d.files > 0 ? 1 : 0)} rx={1.5} className="fill-accent/80">
                <title>{`${d.day}: ${bytes(saved[i])} saved, ${d.files} files, ${d.pages} pages, ${eta(d.seconds)}`}</title>
              </rect>
              {(i === data.length - 1 || (i % 7 === 0 && i < data.length - 4)) && (
                <text
                  x={i === 0 ? x : i === data.length - 1 ? x + bw : x + bw / 2}
                  y={H + 12}
                  textAnchor={i === 0 ? "start" : i === data.length - 1 ? "end" : "middle"}
                  className="fill-muted text-[9px]"
                >
                  {d.day.slice(5)}
                </text>
              )}
            </g>
          );
        })}
      </svg>
    </div>
  );
}
