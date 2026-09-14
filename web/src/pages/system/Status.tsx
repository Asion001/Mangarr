import { useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, CheckCircle2, Info, RefreshCw, XCircle } from "lucide-react";
import { api, unwrap } from "../../api/client";
import { useCommands, useHealth } from "../../api/queries";
import { Badge, Button, Card, Loading, PageHeader, Table, Td, Th } from "../../components/ui";
import { bytes, dateTime, duration, relative } from "../../lib/format";
import { useToast } from "../../lib/toast";

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
      <PageHeader title="Status" />
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
  if (!data) return null;
  return (
    <Card title="Caches" className="mb-6">
      <div className="overflow-x-auto">
        <Table>
          <thead>
            <tr>
              <Th>Cache</Th>
              <Th>Entries</Th>
              <Th>Size</Th>
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
