import { useState } from "react";
import { Link } from "react-router";
import { useQueryClient } from "@tanstack/react-query";
import { Ban, RotateCw, Trash2, Eraser } from "lucide-react";
import { api, unwrap, type Job } from "../../api/client";
import { useQueue } from "../../api/queries";
import { Badge, Button, Confirm, EmptyState, ErrorBox, IconButton, Loading, PageHeader, Progress, Table, Td, Th } from "../../components/ui";
import { relative } from "../../lib/format";
import { useToast } from "../../lib/toast";

const tone = (s: string) => (s === "completed" ? "ok" : s === "failed" ? "err" : s === "queued" ? "default" : "info");

export function QueuePage() {
  const { data, isLoading, error } = useQueue(true);
  const qc = useQueryClient();
  const toast = useToast();
  const [removing, setRemoving] = useState<{ job: Job; blocklist: boolean } | null>(null);

  const retry = async (j: Job) => {
    try {
      await unwrap(api.POST("/api/v1/queue/{id}/retry", { params: { path: { id: j.id } } }));
      qc.invalidateQueries({ queryKey: ["queue"] });
    } catch (e) {
      toast.fromError(e);
    }
  };
  const remove = async () => {
    if (!removing) return;
    try {
      await unwrap(api.DELETE("/api/v1/queue/{id}", { params: { path: { id: removing.job.id }, query: { blocklist: removing.blocklist } } }));
      qc.invalidateQueries({ queryKey: ["queue"] });
      setRemoving(null);
    } catch (e) {
      toast.fromError(e);
    }
  };
  const clear = async () => {
    await unwrap(api.POST("/api/v1/queue/clear-finished"));
    qc.invalidateQueries({ queryKey: ["queue"] });
  };

  return (
    <>
      <PageHeader
        title="Queue"
        subtitle="Chapters being downloaded, processed and imported"
        actions={
          <Button icon={<Eraser className="size-4" />} onClick={clear}>
            Clear finished
          </Button>
        }
      />
      {isLoading && <Loading />}
      {error && <ErrorBox error={error} />}
      {data?.length === 0 && <EmptyState title="Queue is empty">New chapters are queued automatically when a monitored series gets an update.</EmptyState>}
      {data && data.length > 0 && (
        <Table>
          <thead>
            <tr>
              <Th>Series</Th>
              <Th>Chapter</Th>
              <Th>Source</Th>
              <Th>Status</Th>
              <Th>Progress</Th>
              <Th>Updated</Th>
              <Th />
            </tr>
          </thead>
          <tbody>
            {data.map((j) => (
              <tr key={j.id}>
                <Td>
                  <Link to={`/series/${j.seriesId}`} className="font-medium hover:text-accent-2">
                    {j.seriesTitle}
                  </Link>
                </Td>
                <Td>
                  {j.chapter}
                  {j.isUpgrade && <span className="ml-1.5"><Badge tone="info">upgrade</Badge></span>}
                  {j.kind === "reprocess" && <span className="ml-1.5"><Badge tone="accent">upscale</Badge></span>}
                </Td>
                <Td className="text-muted">
                  {j.sourceName}
                  {j.scanlator && ` · ${j.scanlator}`}
                </Td>
                <Td>
                  <Badge tone={tone(j.status)}>{j.status}</Badge>
                  {j.attempt > 0 && j.status !== "completed" && <span className="ml-1 text-xs text-muted">try {j.attempt + 1}</span>}
                  {j.error && (
                    <div className="mt-1 max-w-sm truncate text-xs text-err" title={j.error}>
                      {j.error}
                    </div>
                  )}
                </Td>
                <Td className="w-40">
                  <Progress value={j.progress} tone={j.status === "failed" ? "err" : j.status === "completed" ? "ok" : "accent"} />
                  <div className="mt-1 text-xs text-muted">
                    {j.pagesDone}/{j.pagesTotal} pages
                  </div>
                </Td>
                <Td className="whitespace-nowrap text-muted">{relative(j.updatedAt)}</Td>
                <Td className="text-right">
                  <div className="flex justify-end">
                    {j.status === "failed" && (
                      <IconButton title="Retry" onClick={() => retry(j)}>
                        <RotateCw className="size-4" />
                      </IconButton>
                    )}
                    {j.status !== "completed" && (
                      <>
                        <IconButton title="Remove and blocklist this release" onClick={() => setRemoving({ job: j, blocklist: true })}>
                          <Ban className="size-4" />
                        </IconButton>
                        <IconButton title="Remove" onClick={() => setRemoving({ job: j, blocklist: false })}>
                          <Trash2 className="size-4" />
                        </IconButton>
                      </>
                    )}
                  </div>
                </Td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      <Confirm
        open={!!removing}
        title={removing?.blocklist ? "Remove and blocklist" : "Remove from queue"}
        danger
        confirmLabel="Remove"
        message={
          removing?.blocklist
            ? `The release of ${removing.job.seriesTitle} ch. ${removing.job.chapter} will be blocklisted; another source may be tried.`
            : `Remove ${removing?.job.seriesTitle} ch. ${removing?.job.chapter} from the queue?`
        }
        onConfirm={remove}
        onClose={() => setRemoving(null)}
      />
    </>
  );
}
