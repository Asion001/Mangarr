import { useRef, useState } from "react";
import { useNavigate } from "react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ArchiveRestore, Download, Plus, Trash2, Upload } from "lucide-react";
import { api, apiUrl, basePath, unwrap } from "../../api/client";
import { Badge, Button, Confirm, EmptyState, IconButton, Loading, PageHeader, Table, Td, Th } from "../../components/ui";
import { bytes, dateTime } from "../../lib/format";
import { useToast } from "../../lib/toast";

export function BackupsPage() {
  const qc = useQueryClient();
  const toast = useToast();
  const navigate = useNavigate();
  const [creating, setCreating] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [restoring, setRestoring] = useState<string | null>(null);
  const file = useRef<HTMLInputElement>(null);
  const { data, isLoading } = useQuery({ queryKey: ["backups"], queryFn: () => unwrap(api.GET("/api/v1/system/backups")) });
  const create = async () => {
    setCreating(true);
    try {
      await unwrap(api.POST("/api/v1/system/backups"));
      qc.invalidateQueries({ queryKey: ["backups"] });
      toast.success("Backup created");
    } catch (e) {
      toast.fromError(e);
    } finally {
      setCreating(false);
    }
  };
  const upload = async (f: File) => {
    setUploading(true);
    try {
      const r = await fetch(basePath + "/api/v1/system/backups/upload", { method: "POST", headers: { "Content-Type": "application/zip" }, body: f });
      if (!r.ok) {
        const body = await r.json().catch(() => ({}));
        throw new Error(body.detail || `HTTP ${r.status}`);
      }
      qc.invalidateQueries({ queryKey: ["backups"] });
      toast.success("Backup added", "Restore it from the list");
    } catch (e) {
      toast.fromError(e, "Couldn't add the backup");
    } finally {
      setUploading(false);
      if (file.current) file.current.value = "";
    }
  };
  const restore = async () => {
    if (!restoring) return;
    try {
      await unwrap(api.POST("/api/v1/system/backups/{name}/restore", { params: { path: { name: restoring } } }));
      navigate("/system/database");
    } catch (e) {
      toast.fromError(e, "Couldn't restore");
    }
    setRestoring(null);
  };
  const remove = async (name: string) => {
    await unwrap(api.DELETE("/api/v1/system/backups/{name}", { params: { path: { name } } }));
    qc.invalidateQueries({ queryKey: ["backups"] });
  };
  return (
    <>
      <PageHeader
        title="Backups"
        subtitle="The whole database, on SQLite and PostgreSQL alike. A backup restores into either, also on another install."
        actions={
          <>
            <input ref={file} type="file" accept=".zip,application/zip" hidden onChange={(e) => e.target.files?.[0] && upload(e.target.files[0])} />
            <Button icon={<Upload className="size-4" />} loading={uploading} onClick={() => file.current?.click()}>
              Add a backup file
            </Button>
            <Button variant="primary" loading={creating} icon={<Plus className="size-4" />} onClick={create}>
              Back up now
            </Button>
          </>
        }
      />
      {isLoading && <Loading />}
      {data?.length === 0 && <EmptyState title="No backups yet" />}
      {data && data.length > 0 && (
        <Table>
          <thead>
            <tr>
              <Th>Name</Th>
              <Th>Type</Th>
              <Th>Size</Th>
              <Th>Created</Th>
              <Th />
            </tr>
          </thead>
          <tbody>
            {data.map((b) => (
              <tr key={b.name}>
                <Td className="font-mono text-xs">{b.name}</Td>
                <Td>
                  <Badge>{b.type}</Badge>
                </Td>
                <Td>{bytes(b.size)}</Td>
                <Td className="text-muted">{dateTime(b.created)}</Td>
                <Td className="text-right">
                  <div className="flex justify-end">
                    <a href={apiUrl(`api/v1/system/backups/${encodeURIComponent(b.name)}`)} download>
                      <IconButton title="Download">
                        <Download className="size-4" />
                      </IconButton>
                    </a>
                    <IconButton title="Restore" onClick={() => setRestoring(b.name)}>
                      <ArchiveRestore className="size-4" />
                    </IconButton>
                    <IconButton title="Delete" onClick={() => remove(b.name)}>
                      <Trash2 className="size-4" />
                    </IconButton>
                  </div>
                </Td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      <Confirm
        open={!!restoring}
        title="Restore backup"
        danger
        confirmLabel="Replace data and restart"
        message={`All of mangarr's data is replaced with ${restoring}'s, then mangarr restarts. Files in your library aren't touched. Make a backup first if you might want today's data back.`}
        onConfirm={restore}
        onClose={() => setRestoring(null)}
      />
    </>
  );
}
