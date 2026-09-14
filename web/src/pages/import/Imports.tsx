import { useRef, useState } from "react";
import { Link, useNavigate } from "react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { FileUp, Trash2 } from "lucide-react";
import { api, unwrap, type S } from "../../api/client";
import { Badge, Card, Confirm, EmptyState, ErrorBox, IconButton, Loading, PageHeader, Spinner, Table, Td, Th } from "../../components/ui";
import { date, relative } from "../../lib/format";
import { useToast } from "../../lib/toast";

export type ImportResource = S["ImportResource"];

export const useImports = () => useQuery({ queryKey: ["imports"], queryFn: () => unwrap(api.GET("/api/v1/imports")) });

export const formatLabel = (f: string) => (f === "aidoku" ? "Aidoku" : "Mihon / Tachiyomi");

export function statusTone(s: string) {
  return s === "done" ? "ok" : s === "failed" ? "err" : s === "review" ? "accent" : "info";
}

export function statusLabel(s: string) {
  return { mapping: "matching", review: "ready to import", running: "importing", done: "done", failed: "failed" }[s] ?? s;
}

/** ImportsPage uploads backups of other apps and lists earlier imports. */
export function ImportsPage() {
  const { data, isLoading, error } = useImports();
  const qc = useQueryClient();
  const toast = useToast();
  const nav = useNavigate();
  const input = useRef<HTMLInputElement>(null);
  const [dragging, setDragging] = useState(false);
  const [del, setDel] = useState<ImportResource | null>(null);

  const upload = useMutation({
    mutationFn: (file: File) =>
      unwrap(
        api.POST("/api/v1/imports", {
          params: { query: { fileName: file.name } },
          body: file as unknown as string,
          bodySerializer: (b: unknown) => b as BodyInit,
          headers: { "Content-Type": "application/octet-stream" },
        }),
      ),
    onSuccess: (imp) => {
      qc.invalidateQueries({ queryKey: ["imports"] });
      nav(`/import/${imp.id}`);
    },
    onError: (e) => toast.fromError(e, "Couldn't read the backup"),
  });

  const pick = (files: FileList | null) => {
    const f = files?.[0];
    if (f) upload.mutate(f);
  };

  return (
    <>
      <PageHeader title="Import library" subtitle="Restore your library, read chapters and categories from another app's backup" />
      <Card>
        <div
          onDragOver={(e) => (e.preventDefault(), setDragging(true))}
          onDragLeave={() => setDragging(false)}
          onDrop={(e) => (e.preventDefault(), setDragging(false), pick(e.dataTransfer.files))}
          className={`flex flex-col items-center gap-3 rounded-lg border-2 border-dashed p-8 text-center transition ${dragging ? "border-accent bg-accent/5" : "border-border"}`}
        >
          {upload.isPending ? <Spinner className="size-8" /> : <FileUp className="size-8 text-muted" />}
          <div className="text-sm">
            Drop a backup here or{" "}
            <button type="button" className="font-medium text-accent-2 hover:underline" onClick={() => input.current?.click()}>
              choose a file
            </button>
          </div>
          <div className="max-w-xl text-xs text-muted">
            Mihon, Tachiyomi and forks, and Suwayomi: <code>.tachibk</code> / <code>.proto.gz</code> (Mihon: More → Backup and restore → Create backup).
            Aidoku: <code>.aib</code> (Settings → Backups). Nothing is added until you review the matches and start the import.
          </div>
          <input ref={input} type="file" hidden accept=".tachibk,.gz,.proto,.aib,.json,.plist" onChange={(e) => pick(e.target.files)} />
        </div>
      </Card>

      <h2 className="mb-2 mt-6 text-sm font-semibold uppercase tracking-wide text-muted">Earlier imports</h2>
      {isLoading && <Loading />}
      {error && <ErrorBox error={error} />}
      {data && data.length === 0 && <EmptyState title="No imports yet">Upload a backup to see how its manga map to your catalogs.</EmptyState>}
      {data && data.length > 0 && (
        <Table>
          <thead>
            <tr>
              <Th>Backup</Th>
              <Th>App</Th>
              <Th>Status</Th>
              <Th>Manga</Th>
              <Th>Uploaded</Th>
              <Th className="w-10" />
            </tr>
          </thead>
          <tbody>
            {data.map((imp) => (
              <tr key={imp.id} className="hover:bg-panel-2/60">
                <Td>
                  <Link to={`/import/${imp.id}`} className="font-medium hover:text-accent-2">
                    {imp.fileName}
                  </Link>
                  {imp.info.backupDate && <div className="text-xs text-muted">made {date(imp.info.backupDate)}</div>}
                </Td>
                <Td>{formatLabel(imp.format)}</Td>
                <Td>
                  <Badge tone={statusTone(imp.status)}>{statusLabel(imp.status)}</Badge>
                  {imp.progress && <div className="mt-1 text-xs text-muted">{imp.progress}</div>}
                </Td>
                <Td className="text-xs text-muted">
                  {imp.counts.all ?? 0} · {imp.counts.imported ?? 0} imported
                </Td>
                <Td className="text-xs text-muted">{relative(imp.createdAt)}</Td>
                <Td>
                  <IconButton title="Delete import" onClick={() => setDel(imp)} disabled={imp.busy}>
                    <Trash2 className="size-4" />
                  </IconButton>
                </Td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      <Confirm
        open={!!del}
        title="Delete import?"
        message="The import and its matches are removed. Series it already added stay in the library."
        confirmLabel="Delete"
        danger
        onClose={() => setDel(null)}
        onConfirm={async () => {
          if (!del) return;
          try {
            await unwrap(api.DELETE("/api/v1/imports/{id}", { params: { path: { id: del.id } } }));
            qc.invalidateQueries({ queryKey: ["imports"] });
          } catch (e) {
            toast.fromError(e, "Delete failed");
          }
          setDel(null);
        }}
      />
    </>
  );
}
